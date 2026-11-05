package main

import (
	"context"
	"encoding/json"
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
	"github.com/vxcontrol/cloud/anonymizer"
	"github.com/vxcontrol/cloud/anonymizer/patterns"
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
	backfillLegacy := flag.Bool("backfill-legacy", false, "Import agent-generated legacy knowledge into the skill store")
	backfillLimit := flag.Int("backfill-limit", 0, "Maximum legacy documents to import (0 = all)")
	backfillMinChars := flag.Int("backfill-min-chars", 200, "Skip legacy documents shorter than this")
	exportPack := flag.String("export-pack", "", "Export a stored pack by name and exit")
	versionsSlug := flag.String("versions", "", "List the stored versions of a document and exit")
	diffSlug := flag.String("diff", "", "Print the diff between two versions of a document and exit")
	fromVersion := flag.Int("from", 0, "Source version for --diff")
	toVersion := flag.Int("to", 0, "Target version for --diff (0 = current)")
	publishSlug := flag.String("publish", "", "Publish a document by slug to the given status and exit")
	publishTo := flag.String("publish-to", "", "Target status for --publish")
	publishNote := flag.String("note", "", "Review note recorded with --publish")
	duplicates := flag.Bool("duplicates", false, "Print the duplicate report and exit")
	threshold := flag.Float64("threshold", 0, "Similarity threshold for --duplicates (default 0.93)")
	mergeTarget := flag.String("merge", "", "Target slug for a merge and exit")
	mergeSources := flag.String("merge-from", "", "Comma-separated source slugs for --merge")
	engagements := flag.Bool("engagements", false, "Rank the stored engagements against a target fingerprint and exit")
	engagementServices := flag.String("services", "", "Comma-separated service fingerprints for --engagements, such as 22/ssh,443/https")
	engagementOS := flag.String("os", "", "Operating system filter for --engagements")
	engagementPlatform := flag.String("platform", "", "Platform filter for --engagements")
	engagementTags := flag.String("tags", "", "Comma-separated tags for --engagements")
	reanonymize := flag.Bool("reanonymize", false, "Re-anonymise the stored documents with the current anonymiser and exit")
	evalFile := flag.String("eval", "", "Run the golden query set from this YAML file and exit")
	evalKs := flag.String("eval-k", "3,5", "Comma-separated k values reported as recall@k")
	evalMinRecall := flag.Float64("eval-min-recall", 0, "Exit non-zero when recall at the largest k is below this value (0 disables the gate)")
	evalStatuses := flag.String("eval-statuses", "", "Comma-separated statuses searched by --eval (default: every non-deprecated status)")
	evalJSON := flag.Bool("eval-json", false, "Print the --eval report as JSON")
	distillFlowID := flag.Int64("distill-flow", 0, "Distill a completed flow into a draft skill and exit")
	distillRedact := flag.String("distill-redact", "", "Comma-separated literals redacted before the anonymiser (target name, hostname, internal identifiers)")
	distillTags := flag.String("distill-tags", "", "Comma-separated tags added to the distilled draft")
	distillForce := flag.Bool("distill-force", false, "Import even without evidence or when a near duplicate exists")
	stats := flag.Bool("stats", false, "Print the retrieval usage report of the last --stats-days days and exit")
	statsDays := flag.Int("stats-days", 30, "Window in days for --stats and --stale")
	statsJSON := flag.Bool("stats-json", false, "Print the --stats or --stale report as JSON")
	stale := flag.Bool("stale", false, "List the reviewed or canonical documents unused for --stats-days days and exit")
	outDir := flag.String("out", "", "Output directory for --export-pack")
	dryRun := flag.Bool("dry-run", false, "Parse the pack and print the plan without writing")
	flag.Parse()

	readOnly := *searchQuery != "" || *deleteSlug != "" || *backfillLegacy || *exportPack != "" ||
		*versionsSlug != "" || *diffSlug != "" || *publishSlug != "" || *duplicates || *mergeTarget != "" ||
		*engagements || *reanonymize || *evalFile != "" || *distillFlowID > 0 || *stats || *stale
	if *packPath == "" && !readOnly {
		logrus.Fatal("the --pack flag is required unless --search, --delete, --export-pack, --versions, --diff, --publish, --engagements, --reanonymize, --eval, --distill-flow, --stats, --stale or --backfill-legacy is set")
	}
	// Usage reports only read knowledge_events and knowledge_documents: they must
	// work when the embedding service is unreachable.
	usageOnly := *stats || *stale
	userless := *dryRun && !*reanonymize && *evalFile == "" && *distillFlowID == 0
	if *userRef == "" && !userless {
		logrus.Fatal("the --user flag is required unless --dry-run is set")
	}
	if err := godotenv.Load(*envFile); err != nil && !os.IsNotExist(err) {
		logrus.WithError(err).Warn("unable to load the environment file")
	}

	var manifest *packManifest
	var treePack *skills.Pack
	if *packPath != "" {
		fsys, closePack, err := openPackFS(*packPath)
		if err != nil {
			logrus.WithError(err).Fatal("unable to open the pack")
		}
		defer closePack()

		data, err := readPackManifestBytes(fsys)
		if err != nil {
			logrus.WithError(err).Fatal("unable to read the pack manifest")
		}

		if skills.ManifestIsTree(data) {
			treePack, err = skills.ParseTreePack(fsys)
			if err != nil {
				logrus.WithError(err).Fatal("unable to read the pack tree")
			}
			if *dryRun && !*backfillLegacy {
				printTreePlan(treePack, *status, *visibility)
				return
			}
		} else {
			manifest, err = loadManifest(*packPath)
			if err != nil {
				logrus.WithError(err).Fatal("unable to read the pack manifest")
			}
			if *dryRun && !*backfillLegacy {
				printPlan(*packPath, manifest, *status, *visibility)
				return
			}
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

	var embedder embeddings.Embedder
	if !*dryRun && !usageOnly {
		embedder, err = embeddings.New(cfg)
		if err != nil {
			logrus.WithError(err).Fatal("unable to build the embedding client")
		}
		if !embedder.IsAvailable() {
			logrus.Fatal("the embedding client is not available")
		}
	}

	store := skills.NewStore(pool, embedder, cfg.EmbeddingMaxTextBytes)
	if !*dryRun && !usageOnly && !store.IsAvailable() {
		logrus.Fatal("the skill store is not available")
	}

	if *stats {
		if err := runStats(ctx, store, userID, *statsDays, *statsJSON); err != nil {
			logrus.WithError(err).Fatal("unable to build the usage report")
		}
		return
	}

	if *stale {
		if err := runStale(ctx, store, userID, *statsDays, *statsJSON); err != nil {
			logrus.WithError(err).Fatal("unable to list the stale documents")
		}
		return
	}

	if *backfillLegacy {
		if err := backfillLegacyKnowledge(ctx, pool, store, userID, *backfillLimit, *backfillMinChars, *dryRun); err != nil {
			logrus.WithError(err).Fatal("unable to backfill legacy knowledge")
		}
		return
	}

	if *evalFile != "" {
		if err := runEval(ctx, store, userID, *evalFile, *evalKs, *evalStatuses, *evalMinRecall, *evalJSON); err != nil {
			logrus.WithError(err).Fatal("evaluation failed")
		}
		return
	}

	if *distillFlowID > 0 {
		if err := runDistill(ctx, pool, store, cfg, userID, *distillFlowID,
			splitList(*distillRedact), splitList(*distillTags), *distillForce, *dryRun); err != nil {
			logrus.WithError(err).Fatal("distillation failed")
		}
		return
	}

	if *exportPack != "" {
		if err := exportTreePack(ctx, store, cfg, userID, *exportPack, *outDir); err != nil {
			logrus.WithError(err).Fatal("unable to export the pack")
		}
		return
	}

	if *duplicates {
		if err := printDuplicates(ctx, store, userID, *threshold); err != nil {
			logrus.WithError(err).Fatal("unable to build the duplicate report")
		}
		return
	}

	if *engagements {
		if err := printEngagementMatches(ctx, store, userID, *engagementServices, *engagementOS, *engagementPlatform, *engagementTags); err != nil {
			logrus.WithError(err).Fatal("unable to rank the engagements")
		}
		return
	}

	if *reanonymize {
		if err := reanonymizeDocuments(ctx, store, cfg, userID, *onlySlugs, *dryRun); err != nil {
			logrus.WithError(err).Fatal("unable to re-anonymise the documents")
		}
		return
	}

	if *mergeTarget != "" {
		sources := splitList(*mergeSources)
		result, err := store.MergeDocuments(ctx, userID, *mergeTarget, sources)
		if err != nil {
			logrus.WithError(err).Fatal("unable to merge the documents")
		}
		logrus.WithFields(logrus.Fields{
			"target":  result.Target.Slug,
			"version": result.Version,
			"sources": len(result.Sources),
			"skipped": len(result.Skipped),
			"files":   result.Files,
		}).Info("documents merged")
		return
	}

	if *versionsSlug != "" {
		if err := printVersions(ctx, store, userID, *versionsSlug); err != nil {
			logrus.WithError(err).Fatal("unable to list the versions")
		}
		return
	}

	if *diffSlug != "" {
		if err := printDiff(ctx, store, userID, *diffSlug, *fromVersion, *toVersion); err != nil {
			logrus.WithError(err).Fatal("unable to diff the versions")
		}
		return
	}

	if *publishSlug != "" {
		doc, err := store.Publish(ctx, userID, *publishSlug, *publishTo, *publishNote)
		if err != nil {
			logrus.WithError(err).Fatal("unable to publish the document")
		}
		logrus.WithFields(logrus.Fields{"slug": doc.Slug, "status": doc.Status}).Info("document published")
		return
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

	if treePack != nil {
		if err := importTreePack(ctx, store, cfg, userID, treePack, *status, *visibility); err != nil {
			logrus.WithError(err).Fatal("unable to import the pack")
		}
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

const backfillLegacyQuery = `
SELECT uuid::text,
       cmetadata->>'doc_type',
       COALESCE(cmetadata->>'guide_type', cmetadata->>'answer_type', cmetadata->>'code_lang', ''),
       COALESCE(cmetadata->>'question', ''),
       document
FROM langchain_pg_embedding
WHERE cmetadata->>'doc_type' IN ('guide', 'answer', 'code')
  AND COALESCE(cmetadata->>'manual', '') <> 'true'
  AND length(document) >= $1
ORDER BY length(document) DESC, uuid::text`

// backfillLegacyKnowledge imports agent-generated guide, answer and code rows
// into the skill store as draft references. It keeps the longest copy of each
// normalized question and never publishes anything. The curated manual rows are
// handled by the pack importer, not here.
func backfillLegacyKnowledge(ctx context.Context, pool *pgxpool.Pool, store *skills.Store, userID int64, limit, minChars int, dryRun bool) error {
	rows, err := pool.Query(ctx, backfillLegacyQuery, minChars)
	if err != nil {
		return fmt.Errorf("query legacy knowledge: %w", err)
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	imported, skipped, failed := 0, 0, 0

	for rows.Next() {
		var (
			legacyID, docType, subtype, question, content string
		)
		if err := rows.Scan(&legacyID, &docType, &subtype, &question, &content); err != nil {
			return fmt.Errorf("scan legacy knowledge: %w", err)
		}

		question = strings.TrimSpace(question)
		if question == "" {
			question = docType + " legacy document"
		}
		key := strings.ToLower(strings.Join(strings.Fields(question), " "))
		if _, ok := seen[key]; ok {
			skipped++
			continue
		}
		seen[key] = struct{}{}

		if limit > 0 && imported >= limit {
			break
		}

		if dryRun {
			imported++
			if imported <= 20 {
				fmt.Printf("  %-8s %-13s %7d  %s\n", docType, subtype, len(content), question)
			}
			continue
		}

		_, err := store.ImportMarkdown(ctx, userID, skills.ImportInput{
			Content:     content,
			Title:       question,
			Kind:        skills.KindReference,
			Status:      skills.StatusDraft,
			Visibility:  skills.VisibilityPrivate,
			SourceKind:  "legacy-agent",
			SourceURI:   "pentagi://langchain_pg_embedding/" + legacyID,
			Language:    "en",
			DefaultTags: []string{"legacy-agent", docType, subtype},
		})
		if err != nil {
			logrus.WithError(err).WithField("legacy_id", legacyID).Warn("unable to import legacy knowledge")
			failed++
			continue
		}
		imported++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy knowledge: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"dry_run":   dryRun,
		"failed":    failed,
		"imported":  imported,
		"min_chars": minChars,
		"skipped":   skipped,
	}).Info("legacy backfill finished")

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func printVersions(ctx context.Context, store *skills.Store, userID int64, slug string) error {
	versions, err := store.ListVersions(ctx, userID, slug)
	if err != nil {
		return err
	}

	fmt.Printf("%-8s %-10s %8s  %-20s %s\n", "version", "status", "chars", "created", "hash")
	for _, version := range versions {
		hash := version.ContentHash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		fmt.Printf("%-8d %-10s %8d  %-20s %s\n",
			version.Version, version.Status, version.Chars,
			version.CreatedAt.Format("2006-01-02 15:04:05"), hash)
	}

	return nil
}

func printDiff(ctx context.Context, store *skills.Store, userID int64, slug string, from, to int) error {
	diff, err := store.DiffVersions(ctx, userID, slug, from, to)
	if err != nil {
		return err
	}
	if !diff.Changed {
		fmt.Printf("v%d and v%d are identical\n", diff.From, diff.To)
		return nil
	}

	fmt.Print(diff.Diff)
	fmt.Printf("\n%d added, %d removed\n", diff.Added, diff.Removed)
	return nil
}

func printDuplicates(ctx context.Context, store *skills.Store, userID int64, threshold float64) error {
	report, err := store.DuplicateReport(ctx, userID, threshold)
	if err != nil {
		return err
	}

	fmt.Printf("threshold %.2f, %d similar pairs, %d groups, %d exact groups\n",
		report.Threshold, len(report.Pairs), len(report.Groups), len(report.Exact))

	for _, group := range report.Groups {
		fmt.Printf("\n%s group, max %.4f\n", group.Kind, group.MaxSimilarity)
		for _, doc := range group.Documents {
			fmt.Printf("  %-50s v%d %s (%d chars)\n", doc.Slug, doc.Version, doc.Status, doc.Chars)
		}
	}

	for _, group := range report.Exact {
		fmt.Printf("\nexact duplicates\n")
		for _, doc := range group {
			fmt.Printf("  %-50s %s\n", doc.Slug, doc.Kind)
		}
	}

	return nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseIntList(value string) ([]int, error) {
	parts := splitList(value)
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number <= 0 {
			return nil, fmt.Errorf("invalid k %q", part)
		}
		out = append(out, number)
	}
	return out, nil
}

// runEval runs the golden query set and returns an error when the recall gate
// is not met, so a CI job can fail on a regression.
func runEval(ctx context.Context, store *skills.Store, userID int64, path, ksList, statuses string, minRecall float64, asJSON bool) error {
	dataset, err := skills.LoadEvalFile(path)
	if err != nil {
		return err
	}

	ks, err := parseIntList(ksList)
	if err != nil {
		return err
	}

	report, err := store.Evaluate(ctx, userID, dataset, skills.EvalOptions{
		Ks:       ks,
		Statuses: splitList(statuses),
	})
	if err != nil {
		return err
	}

	if asJSON {
		data, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("dataset: %s\n", path)
		printEvalReport(report)
	}

	if minRecall > 0 && !report.Pass(minRecall) {
		return fmt.Errorf("recall@%d is below the target %.2f", report.Ks[len(report.Ks)-1], minRecall)
	}

	return nil
}

func printEvalReport(report *skills.EvalReport) {
	if report.Corpus != "" {
		fmt.Printf("corpus: %s\n", report.Corpus)
	}

	fmt.Printf("%d cases", report.Cases)
	for _, k := range report.Ks {
		fmt.Printf("  recall@%d %.3f", k, report.Recall[k])
	}
	fmt.Printf("  MRR %.3f  empty %.3f\n", report.MRR, report.EmptyRate)

	fmt.Printf("\n%-24s %4s", "category", "n")
	for _, k := range report.Ks {
		fmt.Printf("  recall@%-2d", k)
	}
	fmt.Printf("      MRR  empty\n")
	for _, category := range report.Categories {
		fmt.Printf("%-24s %4d", category.Category, category.Cases)
		for _, k := range report.Ks {
			fmt.Printf("  %9.3f", category.Recall[k])
		}
		fmt.Printf("  %6.3f  %5.3f\n", category.MRR, category.EmptyRate)
	}

	lastK := report.Ks[len(report.Ks)-1]
	misses := make([]skills.EvalCaseResult, 0)
	for _, result := range report.Results {
		if result.Rank == 0 || result.Rank > lastK {
			misses = append(misses, result)
		}
	}
	if len(misses) == 0 {
		return
	}

	fmt.Printf("\nmisses (not found or below recall@%d):\n", lastK)
	for _, miss := range misses {
		fmt.Printf("  %-30s %s\n", miss.ID, miss.Query)
		if len(miss.TopSlugs) > 0 {
			fmt.Printf("    top: %s\n", strings.Join(miss.TopSlugs, ", "))
		}
	}
}

func runStats(ctx context.Context, store *skills.Store, userID int64, days int, asJSON bool) error {
	report, err := store.UsageReport(ctx, userID, days)
	if err != nil {
		return err
	}

	if asJSON {
		return printJSON(report)
	}

	printUsageReport(report)
	return nil
}

func printUsageReport(report *skills.UsageReport) {
	fmt.Printf("window: %d days\n", report.Days)
	fmt.Printf("%d search(es), %d empty (%.1f%%), %d suggestion(s), %d read(s), %.1f%% of reads followed a suggestion\n",
		report.Searches, report.EmptySearches, report.EmptyRate*100,
		report.Suggestions, report.Reads, report.ReadAfterSuggestion*100)

	if len(report.Skills) == 0 {
		fmt.Println("\nno document is visible in this scope")
		return
	}

	fmt.Printf("\n%-40s %-11s %-10s %8s %6s  %s\n", "slug", "kind", "status", "suggest", "reads", "last seen")
	for _, usage := range report.Skills {
		lastSeen := "-"
		if usage.LastSeen != nil {
			lastSeen = usage.LastSeen.UTC().Format("2006-01-02")
		}
		fmt.Printf("%-40s %-11s %-10s %8d %6d  %s\n",
			usage.Slug, usage.Kind, usage.Status, usage.Suggestions, usage.Reads, lastSeen)
	}

	if len(report.Dead) > 0 {
		fmt.Printf("\nnever suggested or read in the window (%d):\n", len(report.Dead))
		for _, usage := range report.Dead {
			fmt.Printf("  %s [%s/%s]\n", usage.Slug, usage.Kind, usage.Status)
		}
	}
}

// runStale lists the documents that deserve a review: reviewed or canonical,
// unused for the window, and unchanged for as long. Deprecation stays manual.
func runStale(ctx context.Context, store *skills.Store, userID int64, days int, asJSON bool) error {
	report, err := store.UsageReport(ctx, userID, days)
	if err != nil {
		return err
	}

	stale := skills.StaleDocuments(report)
	if asJSON {
		return printJSON(stale)
	}

	fmt.Printf("stale candidates: reviewed or canonical, no suggestion, no read, unchanged for at least %d days\n", report.Days)
	if len(stale) == 0 {
		fmt.Println("\nnone")
		return nil
	}

	for _, usage := range stale {
		fmt.Printf("  %-40s %-10s updated %s\n",
			usage.Slug, usage.Status, usage.UpdatedAt.UTC().Format("2006-01-02"))
	}
	fmt.Printf("\n%d candidate(s); retire one with --publish <slug> --publish-to deprecated --note \"...\"\n", len(stale))

	return nil
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printEngagementMatches(ctx context.Context, store *skills.Store, userID int64, services, osName, platform, tags string) error {
	matches, err := store.SearchEngagements(ctx, userID, skills.EngagementQuery{
		Services: splitList(services),
		OS:       osName,
		Platform: platform,
		Tags:     splitList(tags),
	})
	if err != nil {
		return err
	}

	fmt.Printf("%-7s %-10s %-50s %s\n", "score", "outcome", "slug", "services")
	for _, match := range matches {
		fmt.Printf("%-7.2f %-10s %-50s %s\n",
			match.Score, match.Document.MetadataString("outcome"), match.Document.Slug,
			strings.Join(match.Document.MetadataStrings("services"), ", "))
	}
	fmt.Printf("\n%d engagement(s)\n", len(matches))

	return nil
}

// reanonymizeDocuments applies the same anonymiser the agent tools use. A real
// run imports a new version per changed document, so every edit stays diffable.
func reanonymizeDocuments(ctx context.Context, store *skills.Store, cfg *config.Config, userID int64, onlySlugs string, dryRun bool) error {
	replacer, err := buildReplacer(cfg)
	if err != nil {
		return err
	}

	documents, err := gatherReanonymizeDocuments(ctx, store, userID, onlySlugs)
	if err != nil {
		return err
	}

	changed, skipped, foreign, flags := 0, 0, 0, 0
	for _, doc := range documents {
		if doc.UserID != userID {
			foreign++
			continue
		}

		if dryRun {
			redacted := skills.RedactDocument(doc, replacer)
			if !redacted.Changed(doc) {
				skipped++
				continue
			}
			changed++
			flags += redacted.Flags
			fmt.Printf("%-50s would change (title %t, description %t, content %t, flags %d)\n",
				doc.Slug, redacted.Title != doc.Title, redacted.Description != doc.Description,
				redacted.Content != doc.Content, redacted.Flags)
			continue
		}

		result, err := store.Reanonymize(ctx, userID, doc.Slug, replacer)
		if err != nil {
			return fmt.Errorf("re-anonymise %q: %w", doc.Slug, err)
		}
		if result.Skipped {
			skipped++
			continue
		}
		changed++
		flags += result.Flags
		fmt.Printf("%-50s v%d (title %t, description %t, content %t, flags %d)\n",
			doc.Slug, result.Version, result.Title, result.Description, result.Content, result.Flags)
	}

	logrus.WithFields(logrus.Fields{
		"dry_run": dryRun,
		"changed": changed,
		"skipped": skipped,
		"flags":   flags,
		"foreign": foreign,
	}).Info("re-anonymisation finished")

	return nil
}

func gatherReanonymizeDocuments(ctx context.Context, store *skills.Store, userID int64, onlySlugs string) ([]skills.Document, error) {
	only := splitList(onlySlugs)
	if len(only) > 0 {
		documents := make([]skills.Document, 0, len(only))
		for _, slug := range only {
			doc, err := store.Get(ctx, userID, slug)
			if err != nil {
				return nil, fmt.Errorf("read %q: %w", slug, err)
			}
			documents = append(documents, *doc)
		}
		return documents, nil
	}

	// Every status is listed on purpose: a deprecated engagement still has to be
	// scrubbed. The store caps one list, so a larger scope takes --only batches.
	return store.List(ctx, userID, skills.Filter{
		Statuses: []string{skills.StatusDraft, skills.StatusReviewed, skills.StatusCanonical, skills.StatusDeprecated},
		Limit:    skills.MaxListLimit,
	})
}

func buildReplacer(cfg *config.Config) (anonymizer.Replacer, error) {
	allPatterns, err := patterns.LoadPatterns(patterns.PatternListTypeAll)
	if err != nil {
		return nil, fmt.Errorf("load anonymiser patterns: %w", err)
	}
	allPatterns.Patterns = append(allPatterns.Patterns, cfg.GetSecretPatterns()...)

	replacer, err := anonymizer.NewReplacer(allPatterns.Regexes(), allPatterns.Names())
	if err != nil {
		return nil, fmt.Errorf("build anonymiser: %w", err)
	}

	return replacer, nil
}
