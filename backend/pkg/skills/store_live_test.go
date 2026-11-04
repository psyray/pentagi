//go:build postgres

package skills

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pentagi/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

type unitEmbedder struct{}

func (unitEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = unitVector()
	}
	return out, nil
}

func (unitEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return unitVector(), nil
}

func (unitEmbedder) IsAvailable() bool { return true }

func unitVector() []float32 {
	vector := make([]float32, embeddingDimension)
	vector[0] = 1
	return vector
}

func openSkillsSchema(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	dsn := os.Getenv("PENTAGI_TEST_DSN")
	if dsn == "" {
		t.Fatal("PENTAGI_TEST_DSN is unset; this tier needs a postgres to prove anything")
	}

	admin, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("skills_%d", time.Now().UnixNano())
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("open scoped pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// goose needs a database/sql handle for the version table and the
	// non-transactional index statements.
	sqlDB, err := openSQL(dsn, schema)
	if err != nil {
		t.Fatalf("open sql: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	goose.SetBaseFS(migrations.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	goose.SetTableName(schema + ".goose_db_version")
	if err := goose.Up(sqlDB, "sql"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return pool, schema
}

func openSQL(dsn, schema string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("SET search_path TO " + schema + ",public"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func TestStore_LiveImportSearchReadDelete(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	content := `---
name: adcs-esc8
description: Relay NTLM to ADCS HTTP enrollment.
tags: [adcs, esc8, ntlm]
---
# ADCS ESC8

## Steps

run certipy relay against the enrollment endpoint

## Limits

none
`

	doc, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: content, Kind: KindSkill})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if doc.Slug != "adcs-esc8" || doc.Version != 1 {
		t.Fatalf("imported %+v", doc)
	}

	var chunks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_chunks WHERE document_id = $1::uuid`, doc.ID).Scan(&chunks); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunks == 0 {
		t.Fatal("no chunk was written")
	}

	again, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: content, Kind: KindSkill})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again.Version != 2 {
		t.Fatalf("re-import version = %d, want 2", again.Version)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_chunks WHERE document_id = $1::uuid`, doc.ID).Scan(&after); err != nil {
		t.Fatalf("count chunks after re-import: %v", err)
	}
	if after != chunks {
		t.Fatalf("re-import changed the chunk count from %d to %d", chunks, after)
	}

	hits, err := store.Search(ctx, 1, "certipy esc8 relay", Filter{}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Document.Slug != "adcs-esc8" {
		t.Fatalf("search hits = %+v", hits)
	}
	if !strings.Contains(hits[0].Excerpt, "certipy") {
		t.Errorf("excerpt does not carry the matched text: %q", hits[0].Excerpt)
	}

	full, files, err := store.Read(ctx, 1, "adcs-esc8")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(full.Content, "certipy relay") || len(files) != 0 {
		t.Errorf("read returned %+v with %d files", full, len(files))
	}

	list, err := store.List(ctx, 1, Filter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err := store.Delete(ctx, 1, "adcs-esc8"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, 1, "adcs-esc8"); err != ErrNotFound {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestStore_LiveAnotherUserCannotReadAPrivateSkill(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	doc, err := store.ImportMarkdown(ctx, 1, ImportInput{
		Content: "---\nname: private-skill\ndescription: private\n---\n# Private\n\nbody\n",
		Kind:    KindSkill,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if _, err := store.Get(ctx, 2, doc.Slug); err != ErrNotFound {
		t.Fatalf("another user read the private skill: %v", err)
	}

	hits, err := store.Search(ctx, 2, "private", Filter{}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("another user found %d private hits", len(hits))
	}
}

func TestStore_LiveFileLifecycle(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	doc, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: "# ADCS\n\nbody\n", Kind: KindReference})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	file, err := store.AddFile(ctx, 1, doc.ID, File{
		Path:    "scripts/relay.py",
		MIME:    "text/x-python",
		Size:    12,
		SHA256:  "abc",
		BlobRef: "0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("add file: %v", err)
	}
	if file.Path != "scripts/relay.py" {
		t.Fatalf("added file = %+v", file)
	}

	files, err := store.Files(ctx, doc.ID)
	if err != nil || len(files) != 1 || files[0].BlobRef != file.BlobRef {
		t.Fatalf("files = %+v, %v", files, err)
	}

	got, err := store.GetFile(ctx, 1, doc.ID, "scripts/relay.py")
	if err != nil || got.BlobRef != file.BlobRef {
		t.Fatalf("get file = %+v, %v", got, err)
	}

	if _, err := store.AddFile(ctx, 2, doc.ID, File{Path: "evil.sh", BlobRef: file.BlobRef}); err != ErrNotFound {
		t.Fatalf("another user add file = %v, want ErrNotFound", err)
	}

	blobRef, err := store.DeleteFile(ctx, 1, doc.ID, "scripts/relay.py")
	if err != nil || blobRef != file.BlobRef {
		t.Fatalf("delete file = %q, %v", blobRef, err)
	}

	referenced, err := store.BlobReferenced(ctx, blobRef)
	if err != nil || referenced {
		t.Fatalf("blob referenced = %v, %v", referenced, err)
	}
}

func TestStore_LiveUpdateMetadata(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	doc, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: "# Update\n\nbody\n", Kind: KindReference})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	status := StatusReviewed
	visibility := VisibilityShared
	description := "reviewed reference"
	tags := []string{"adcs", "esc8"}

	updated, err := store.UpdateMetadata(ctx, 1, doc.Slug, MetadataInput{
		Status:      &status,
		Visibility:  &visibility,
		Description: &description,
		Tags:        tags,
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Status != StatusReviewed || updated.Visibility != VisibilityShared || updated.Description != description {
		t.Fatalf("updated = %+v", updated)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "adcs" {
		t.Fatalf("updated tags = %v", updated.Tags)
	}

	if _, err := store.UpdateMetadata(ctx, 2, doc.Slug, MetadataInput{Status: &status}); err != ErrNotFound {
		t.Fatalf("another user update = %v, want ErrNotFound", err)
	}

	draft := StatusDraft
	cleared, err := store.UpdateMetadata(ctx, 1, doc.Slug, MetadataInput{Status: &draft, Tags: []string{}})
	if err != nil {
		t.Fatalf("clear metadata: %v", err)
	}
	if cleared.Status != StatusDraft || len(cleared.Tags) != 0 {
		t.Fatalf("cleared = %+v", cleared)
	}
}
