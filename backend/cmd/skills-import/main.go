package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pentagi/pkg/config"
	"pentagi/pkg/providers/embeddings"
	"pentagi/pkg/skills"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// packManifest is the pack.yaml format: a JSON or YAML document listing the
// markdown files to import, with their metadata. Only the fields the importer
// needs are declared; the rest is kept in the file for humans.
type packManifest struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Kind        string         `yaml:"kind"`
	Status      string         `yaml:"status"`
	Visibility  string         `yaml:"visibility"`
	Documents   []packDocument `yaml:"documents"`
}

type packDocument struct {
	Slug       string   `yaml:"slug"`
	File       string   `yaml:"file"`
	Title      string   `yaml:"title"`
	Kind       string   `yaml:"kind"`
	Status     string   `yaml:"status"`
	Visibility string   `yaml:"visibility"`
	SourceKind string   `yaml:"source_kind"`
	SourceURI  string   `yaml:"source_uri"`
	Tags       []string `yaml:"tags"`
}

func main() {
	envFile := flag.String("env", ".env", "Path to environment file")
	packPath := flag.String("pack", "", "Path to the pack directory containing pack.yaml")
	userRef := flag.String("user", "", "Target user email or numeric id")
	status := flag.String("status", "", "Override the document status")
	visibility := flag.String("visibility", "", "Override the document visibility")
	sourceKind := flag.String("source-kind", "", "Override the document source kind")
	databaseURL := flag.String("database-url", "", "Override DATABASE_URL")
	searchQuery := flag.String("search", "", "Run a search query and print the hits")
	deleteSlug := flag.String("delete", "", "Soft-delete a document by slug and exit")
	onlySlugs := flag.String("only", "", "Comma-separated slugs to import (default: all)")
	dryRun := flag.Bool("dry-run", false, "Parse the pack and print the plan without writing")
	flag.Parse()

	if *packPath == "" && *searchQuery == "" && *deleteSlug == "" {
		logrus.Fatal("the --pack flag is required unless --search or --delete is set")
	}
	if *userRef == "" && !*dryRun {
		logrus.Fatal("the --user flag is required unless --dry-run is set")
	}
	if err := godotenv.Load(*envFile); err != nil && !os.IsNotExist(err) {
		logrus.WithError(err).Warn("unable to load the environment file")
	}

	var manifest *packManifest
	if *packPath != "" {
		var err error
		manifest, err = loadManifest(*packPath)
		if err != nil {
			logrus.WithError(err).Fatal("unable to read the pack manifest")
		}

		if *dryRun {
			printPlan(*packPath, manifest, *status, *visibility)
			return
		}
	}

	cfg, err := config.NewConfig()
	if err != nil {
		logrus.WithError(err).Fatal("unable to read the configuration")
	}
	if *databaseURL != "" {
		cfg.DatabaseURL = *databaseURL
	}
	if cfg.DatabaseURL == "" {
		logrus.Fatal("DATABASE_URL is not set")
	}

	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logrus.WithError(err).Fatal("unable to parse the database URL")
	}
	poolConfig.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		logrus.WithError(err).Fatal("unable to connect to the database")
	}
	defer pool.Close()

	userID, err := resolveUser(ctx, pool, *userRef)
	if err != nil {
		logrus.WithError(err).Fatal("unable to resolve the target user")
	}

	embedder, err := embeddings.New(cfg)
	if err != nil {
		logrus.WithError(err).Fatal("unable to build the embedding client")
	}
	if !embedder.IsAvailable() {
		logrus.Fatal("the embedding client is not available")
	}

	store := skills.NewStore(pool, embedder, cfg.EmbeddingMaxTextBytes)
	if !store.IsAvailable() {
		logrus.Fatal("the skill store is not available")
	}

	if *deleteSlug != "" {
		if err := store.Delete(ctx, userID, *deleteSlug); err != nil {
			logrus.WithError(err).Fatal("unable to delete the document")
		}
		logrus.WithField("slug", *deleteSlug).Info("document deleted")
		return
	}

	only := map[string]struct{}{}
	for _, slug := range strings.Split(*onlySlugs, ",") {
		if slug = strings.TrimSpace(slug); slug != "" {
			only[slug] = struct{}{}
		}
	}

	imported, failed := 0, 0
	if manifest != nil {
		for _, doc := range manifest.Documents {
			if len(only) > 0 {
				if _, ok := only[doc.Slug]; !ok {
					continue
				}
			}
			path := filepath.Join(*packPath, doc.File)
			content, err := os.ReadFile(path)
			if err != nil {
				logrus.WithError(err).WithField("file", doc.File).Error("unable to read the document")
				failed++
				continue
			}

			document, err := store.ImportMarkdown(ctx, userID, skills.ImportInput{
				Content:     string(content),
				Title:       doc.Title,
				Kind:        firstNonEmpty(doc.Kind, manifest.Kind),
				Status:      firstNonEmpty(*status, doc.Status, manifest.Status),
				Visibility:  firstNonEmpty(*visibility, doc.Visibility, manifest.Visibility),
				SourceKind:  firstNonEmpty(*sourceKind, doc.SourceKind, "pack"),
				SourceURI:   doc.SourceURI,
				Language:    "en",
				DefaultTags: doc.Tags,
			})
			if err != nil {
				logrus.WithError(err).WithField("file", doc.File).Error("unable to import the document")
				failed++
				continue
			}

			imported++
			logrus.WithFields(logrus.Fields{
				"slug":    document.Slug,
				"kind":    document.Kind,
				"version": document.Version,
			}).Info("document imported")
		}

		logrus.WithFields(logrus.Fields{
			"pack":     manifest.Name,
			"version":  manifest.Version,
			"imported": imported,
			"failed":   failed,
		}).Info("import finished")
	}

	if *searchQuery != "" {
		hits, err := store.Search(ctx, userID, *searchQuery, skills.Filter{}, 5)
		if err != nil {
			logrus.WithError(err).Fatal("unable to run the search")
		}
		for _, hit := range hits {
			fmt.Printf("%.4f  %-10s %-60s  %s\n",
				hit.Score, hit.Document.Kind, hit.Document.Title, hit.Excerpt)
		}
	}

	if failed > 0 {
		os.Exit(1)
	}
}

func loadManifest(packPath string) (*packManifest, error) {
	data, err := os.ReadFile(filepath.Join(packPath, "pack.yaml"))
	if err != nil {
		return nil, err
	}

	var manifest packManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse pack.yaml: %w", err)
	}
	if len(manifest.Documents) == 0 {
		return nil, fmt.Errorf("pack.yaml has no documents")
	}

	return &manifest, nil
}

func printPlan(packPath string, manifest *packManifest, status, visibility string) {
	fmt.Printf("pack: %s %s\n", manifest.Name, manifest.Version)
	fmt.Printf("documents: %d\n", len(manifest.Documents))
	for _, doc := range manifest.Documents {
		path := filepath.Join(packPath, doc.File)
		info, err := os.Stat(path)
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		fmt.Printf("  %-16s %-10s %-10s %7d  %s\n",
			firstNonEmpty(doc.Kind, manifest.Kind),
			firstNonEmpty(status, doc.Status, manifest.Status),
			firstNonEmpty(visibility, doc.Visibility, manifest.Visibility),
			size,
			strings.TrimSpace(firstNonEmpty(doc.Title, doc.File)),
		)
	}
}

func resolveUser(ctx context.Context, pool *pgxpool.Pool, ref string) (int64, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return id, nil
	}

	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE mail = $1`, ref).Scan(&id); err != nil {
		return 0, fmt.Errorf("user %q: %w", ref, err)
	}

	return id, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
