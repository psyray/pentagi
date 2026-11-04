//go:build postgres

package skills

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
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

func TestStore_LiveCatalogue(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	importDoc := func(slug, status, description string) {
		content := fmt.Sprintf("---\nname: %s\nstatus: %s\ndescription: %s\ntags: [adcs]\n---\n# %s\n\nbody of %s\n",
			slug, status, description, slug, slug)
		if _, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: content, Kind: KindSkill}); err != nil {
			t.Fatalf("import %s: %v", slug, err)
		}
	}
	importDoc("canonical-skill", StatusCanonical, "Canonical procedure.")
	importDoc("reviewed-skill", StatusReviewed, "Reviewed procedure.")
	importDoc("draft-skill", StatusDraft, "Draft procedure.")

	catalogue, err := store.BuildCatalogue(ctx, 1, CatalogueOptions{Query: "adcs escalation", Limit: 10})
	if err != nil {
		t.Fatalf("build catalogue: %v", err)
	}
	if len(catalogue.Entries) != 2 {
		t.Fatalf("catalogue entries = %d, want 2: %+v", len(catalogue.Entries), catalogue.Entries)
	}
	if catalogue.Entries[0].Slug != "canonical-skill" {
		t.Errorf("canonical skill is not first: %+v", catalogue.Entries)
	}
	if catalogue.Hash == "" || !strings.Contains(catalogue.Text, "read_skill") {
		t.Errorf("catalogue is incomplete: hash=%q text=%q", catalogue.Hash, catalogue.Text)
	}

	withDrafts, err := store.BuildCatalogue(ctx, 1, CatalogueOptions{Query: "adcs escalation", IncludeDrafts: true})
	if err != nil {
		t.Fatalf("build catalogue with drafts: %v", err)
	}
	if len(withDrafts.Entries) != 3 {
		t.Fatalf("catalogue with drafts = %d, want 3", len(withDrafts.Entries))
	}

	unrelated, err := store.BuildCatalogue(ctx, 1, CatalogueOptions{Limit: 10})
	if err != nil {
		t.Fatalf("build catalogue without a query: %v", err)
	}
	if len(unrelated.Entries) != 2 {
		t.Fatalf("list-based catalogue = %d, want 2", len(unrelated.Entries))
	}
}

func TestStore_LivePackImportAndExport(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	fsys := fstest.MapFS{
		"pack.yaml":                     {Data: []byte("name: web-app\ndescription: Playbooks.\nversion: 1.0.0\nvisibility: private\nskills: [ssrf]\nreferences: [wstg]\nmissions: [example]\n")},
		"skills/ssrf/SKILL.md":          {Data: []byte("# SSRF\n\nStart a listener.\n")},
		"skills/ssrf/files/probe.py":    {Data: []byte("print('probe')\n")},
		"references/wstg/chapter-01.md": {Data: []byte("# Chapter one\n\nInjection.\n")},
		"missions/example.md":           {Data: []byte("# Example\n\nAnonymized.\n")},
	}
	pack, err := ParseTreePack(fsys)
	if err != nil {
		t.Fatalf("parse pack: %v", err)
	}

	blobs := map[string][]byte{}
	attach := func(ctx context.Context, userID int64, documentID, path string, content []byte) error {
		digest := md5.Sum(content)
		hash := hex.EncodeToString(digest[:])
		blobs[hash] = content
		_, err := store.AddFile(ctx, userID, documentID, File{
			Path: path, Size: int64(len(content)), SHA256: "sha", BlobRef: hash,
		})
		return err
	}

	result, err := store.ImportPack(ctx, 1, pack, PackImportOptions{AttachFile: attach})
	if err != nil {
		t.Fatalf("import pack: %v", err)
	}
	if len(result.Created) != 3 || result.Files != 1 {
		t.Fatalf("import result = %+v", result)
	}

	again, err := store.ImportPack(ctx, 1, pack, PackImportOptions{AttachFile: attach})
	if err != nil {
		t.Fatalf("re-import pack: %v", err)
	}
	if len(again.Updated) != 3 || again.PackID != result.PackID {
		t.Fatalf("re-import result = %+v", again)
	}

	export, report, err := store.ExportPack(ctx, 1, "web-app", func(hash string) ([]byte, error) {
		content, ok := blobs[hash]
		if !ok {
			return nil, fmt.Errorf("unknown blob %s", hash)
		}
		return content, nil
	})
	if err != nil {
		t.Fatalf("export pack: %v", err)
	}
	if report.Files != 1 {
		t.Errorf("exported files = %d, want 1", report.Files)
	}

	for _, name := range []string{
		"pack.yaml",
		"skills/ssrf/SKILL.md",
		"skills/ssrf/files/probe.py",
		"references/wstg.md",
		"missions/example.md",
	} {
		if _, ok := export.Files[name]; !ok {
			t.Errorf("export is missing %s", name)
		}
	}

	roundTrip := fstest.MapFS{}
	for name, content := range export.Files {
		roundTrip[name] = &fstest.MapFile{Data: content}
	}
	reparsed, err := ParseTreePack(roundTrip)
	if err != nil {
		t.Fatalf("re-parse the export: %v", err)
	}
	if len(reparsed.Documents) != 3 {
		t.Fatalf("re-parsed documents = %d, want 3", len(reparsed.Documents))
	}
}

func TestStore_LiveVersionsAndPublish(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	first := "---\nname: adcs-esc8\ndescription: Relay NTLM to the CA.\ntags: [adcs]\n---\n# ADCS ESC8\n\nRun certipy.\n"
	doc, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: first, Kind: KindSkill})
	if err != nil {
		t.Fatalf("import v1: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("first import version = %d", doc.Version)
	}

	second := strings.Replace(first, "Run certipy.", "Run certipy relay.", 1)
	updated, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: second, Kind: KindSkill})
	if err != nil {
		t.Fatalf("import v2: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("second import version = %d, want 2", updated.Version)
	}

	versions, err := store.ListVersions(ctx, 1, "adcs-esc8")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("versions = %+v", versions)
	}

	revision, err := store.GetVersion(ctx, 1, "adcs-esc8", 1)
	if err != nil {
		t.Fatalf("read v1: %v", err)
	}
	if !strings.Contains(revision.Content, "Run certipy.") || strings.Contains(revision.Content, "relay") {
		t.Fatalf("v1 content = %q", revision.Content)
	}

	diff, err := store.DiffVersions(ctx, 1, "adcs-esc8", 1, 0)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !diff.Changed || !strings.Contains(diff.Diff, "+Run certipy relay.") {
		t.Fatalf("diff = %+v", diff)
	}

	if _, err := store.Publish(ctx, 1, "adcs-esc8", StatusCanonical, "skip the review"); err == nil {
		t.Fatal("a draft reached canonical in one step")
	}
	if _, err := store.Publish(ctx, 1, "adcs-esc8", StatusReviewed, "reviewed by the test"); err != nil {
		t.Fatalf("publish reviewed: %v", err)
	}
	canonical, err := store.Publish(ctx, 1, "adcs-esc8", StatusCanonical, "")
	if err != nil {
		t.Fatalf("publish canonical: %v", err)
	}
	if canonical.Status != StatusCanonical {
		t.Fatalf("status = %s", canonical.Status)
	}
	if _, err := store.Publish(ctx, 2, "adcs-esc8", StatusDeprecated, ""); err != ErrNotFound {
		t.Fatalf("another user publish = %v, want ErrNotFound", err)
	}

	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM knowledge_events e
JOIN knowledge_documents d ON d.id = e.document_id
WHERE d.slug = 'adcs-esc8' AND e.event = 'published'`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 2 {
		t.Fatalf("published events = %d, want 2", events)
	}
}

func TestStore_LiveDuplicatesAndMerge(t *testing.T) {
	pool, _ := openSkillsSchema(t)
	store := NewStore(pool, unitEmbedder{}, 8192)
	ctx := context.Background()

	importDoc := func(slug, kind, content string) {
		front := "---\nname: " + slug + "\ndescription: A " + kind + " for the test.\ntags: [test]\n---\n"
		if _, err := store.ImportMarkdown(ctx, 1, ImportInput{Content: front + content, Kind: kind}); err != nil {
			t.Fatalf("import %s: %v", slug, err)
		}
	}
	importDoc("dup-a", KindSkill, "# Dup A\n\nRelay NTLM to the enrollment endpoint.\n")
	importDoc("dup-b", KindSkill, "# Dup B\n\nRelay NTLM to the enrollment endpoint with certipy.\n")
	importDoc("ref-c", KindReference, "# Ref C\n\nUnrelated reference.\n")

	report, err := store.DuplicateReport(ctx, 1, 0.9)
	if err != nil {
		t.Fatalf("duplicate report: %v", err)
	}
	if len(report.Groups) != 1 || len(report.Groups[0].Documents) != 2 {
		t.Fatalf("groups = %+v", report.Groups)
	}
	if report.Groups[0].Documents[0].Kind != KindSkill {
		t.Fatalf("group kind = %s", report.Groups[0].Documents[0].Kind)
	}

	// Attach a file to the source so the merge has to move it.
	digest := md5.Sum([]byte("probe"))
	hash := hex.EncodeToString(digest[:])
	source, err := store.Get(ctx, 1, "dup-b")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if _, err := store.AddFile(ctx, 1, source.ID, File{
		Path: "files/probe.py", Size: 5, SHA256: "sha", BlobRef: hash,
	}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	result, err := store.MergeDocuments(ctx, 1, "dup-a", []string{"dup-b"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Version != 2 || len(result.Sources) != 1 || result.Files != 1 {
		t.Fatalf("merge result = %+v", result)
	}
	if !strings.Contains(result.Target.Content, "## Merged from `dup-b`") {
		t.Fatalf("target content does not carry the source: %q", result.Target.Content)
	}
	if result.Target.Description != "A skill for the test." {
		t.Fatalf("target description = %q", result.Target.Description)
	}

	mergedSource, err := store.Get(ctx, 1, "dup-b")
	if err != nil {
		t.Fatalf("read merged source: %v", err)
	}
	if mergedSource.Status != StatusDeprecated || metadataString(mergedSource.Metadata, mergedIntoKey) != "dup-a" {
		t.Fatalf("merged source = %+v", mergedSource)
	}
	if mergedFrom := metadataStrings(result.Target.Metadata, mergeMetadataKey); len(mergedFrom) != 1 || mergedFrom[0] != "dup-b" {
		t.Fatalf("merged_from = %v", mergedFrom)
	}

	files, err := store.Files(ctx, result.Target.ID)
	if err != nil {
		t.Fatalf("list target files: %v", err)
	}
	if len(files) != 1 || files[0].Path != "files/merged/dup-b/probe.py" {
		t.Fatalf("target files = %+v", files)
	}

	again, err := store.MergeDocuments(ctx, 1, "dup-a", []string{"dup-b"})
	if err != nil {
		t.Fatalf("merge again: %v", err)
	}
	if len(again.Sources) != 0 || len(again.Skipped) != 1 || again.Version != 2 {
		t.Fatalf("second merge must be a no-op: %+v", again)
	}

	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM knowledge_events e
JOIN knowledge_documents d ON d.id = e.document_id
WHERE d.slug = 'dup-a' AND e.event = 'merged'`).Scan(&events); err != nil {
		t.Fatalf("count merge events: %v", err)
	}
	if events != 1 {
		t.Fatalf("merge events = %d, want 1", events)
	}
}
