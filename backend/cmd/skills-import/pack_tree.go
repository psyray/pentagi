package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"pentagi/pkg/config"
	"pentagi/pkg/resources"
	"pentagi/pkg/skills"

	"github.com/sirupsen/logrus"
)

// openPackFS returns the pack tree as a filesystem: a directory, or the root of
// a zip archive. The second value releases the archive.
func openPackFS(packPath string) (fs.FS, func(), error) {
	info, err := os.Stat(packPath)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		reader, err := zip.OpenReader(packPath)
		if err != nil {
			return nil, nil, fmt.Errorf("open zip pack: %w", err)
		}
		return reader, func() { _ = reader.Close() }, nil
	}

	return os.DirFS(packPath), func() {}, nil
}

// readPackManifestBytes reads pack.yaml from a pack filesystem.
func readPackManifestBytes(fsys fs.FS) ([]byte, error) {
	return fs.ReadFile(fsys, skills.PackManifestFile)
}

// importTreePack imports a parsed pack. Attachments go to the shared resource
// blob store, exactly like an upload from the UI.
func importTreePack(
	ctx context.Context,
	store *skills.Store,
	cfg *config.Config,
	userID int64,
	pack *skills.Pack,
	status, visibility string,
) error {
	attach := func(ctx context.Context, userID int64, documentID, filePath string, content []byte) error {
		tmp, hash, size, err := resources.SaveToTemp(bytes.NewReader(content), resources.ResourcesDir(cfg.DataDir))
		if err != nil {
			return err
		}
		if err := resources.CommitBlob(cfg.DataDir, hash, tmp); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		digest := sha256.Sum256(content)
		_, err = store.AddFile(ctx, userID, documentID, skills.File{
			Path:    filePath,
			MIME:    mime.TypeByExtension(path.Ext(filePath)),
			Size:    size,
			SHA256:  fmt.Sprintf("%x", digest),
			BlobRef: hash,
		})
		return err
	}

	result, err := store.ImportPack(ctx, userID, pack, skills.PackImportOptions{
		Status:     status,
		Visibility: visibility,
		AttachFile: attach,
	})
	if err != nil {
		return err
	}

	for _, warning := range result.Warnings {
		logrus.WithField("pack", result.Name).Warn(warning)
	}
	logrus.WithFields(logrus.Fields{
		"pack":      result.Name,
		"pack_id":   result.PackID,
		"created":   len(result.Created),
		"updated":   len(result.Updated),
		"files":     result.Files,
		"documents": len(pack.Documents),
	}).Info("pack imported")

	return nil
}

// exportTreePack writes a pack back to a directory tree.
func exportTreePack(ctx context.Context, store *skills.Store, cfg *config.Config, userID int64, name, outDir string) error {
	if strings.TrimSpace(outDir) == "" {
		return fmt.Errorf("the --out flag is required with --export-pack")
	}

	export, report, err := store.ExportPack(ctx, userID, name, func(hash string) ([]byte, error) {
		return os.ReadFile(resources.BlobPath(cfg.DataDir, hash))
	})
	if err != nil {
		return err
	}

	for _, warning := range report.Warnings {
		logrus.WithField("pack", name).Warn(warning)
	}

	names := make([]string, 0, len(export.Files))
	for filePath := range export.Files {
		names = append(names, filePath)
	}
	sort.Strings(names)

	for _, filePath := range names {
		target := filepath.Join(outDir, filepath.FromSlash(filePath))
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(outDir)+string(os.PathSeparator)) {
			return fmt.Errorf("refusing to write %q outside %q", filePath, outDir)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, export.Files[filePath], 0644); err != nil {
			return err
		}
	}

	logrus.WithFields(logrus.Fields{
		"pack":  name,
		"files": len(export.Files),
		"out":   outDir,
	}).Info("pack exported")

	return nil
}

// printTreePlan prints what a tree pack import would do.
func printTreePlan(pack *skills.Pack, status, visibility string) {
	fmt.Printf("pack: %s %s\n", pack.Manifest.Name, pack.Manifest.Version)
	fmt.Printf("documents: %d\n", len(pack.Documents))
	for _, doc := range pack.Documents {
		files := len(doc.Files)
		fmt.Printf("  %-10s %-10s %-10s %7d chars %2d files  %s\n",
			doc.Kind,
			firstNonEmpty(status, pack.Manifest.Status),
			firstNonEmpty(visibility, pack.Manifest.Visibility),
			len(doc.Markdown),
			files,
			strings.TrimSpace(doc.Title),
		)
	}
	for _, warning := range pack.Warnings {
		fmt.Printf("  warning: %s\n", warning)
	}
}
