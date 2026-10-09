package services

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"pentagi/pkg/database"
	"pentagi/pkg/flowfiles"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addAttachmentToZip bundles a cache-relative attachment from the host cache,
// mapping it under annexes/attachments/<path> (backlog v2, L6).
func TestReportArchive_AddAttachmentToZipReadsTheHostCache(t *testing.T) {
	dataDir := t.TempDir()
	const flowID = 7

	uploadsDir := flowfiles.FlowUploadsDir(dataDir, flowID)
	require.NoError(t, os.MkdirAll(uploadsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploadsDir, "x.txt"), []byte("hello"), 0o644))

	svc := &ReportArchiveService{dataDir: dataDir}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	err := svc.addAttachmentToZip(context.Background(), zw, flowID, database.ReportSectionAttachment{Path: "uploads/x.txt"})
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	assert.Equal(t, "annexes/attachments/uploads/x.txt", zr.File[0].Name)
}

func TestReportArchive_AddAttachmentToZipRefusesMissingBlankAndEscapingPaths(t *testing.T) {
	svc := &ReportArchiveService{dataDir: t.TempDir()}
	zw := zip.NewWriter(io.Discard)

	err := svc.addAttachmentToZip(context.Background(), zw, 7, database.ReportSectionAttachment{Path: "uploads/missing.txt"})
	require.Error(t, err)

	err = svc.addAttachmentToZip(context.Background(), zw, 7, database.ReportSectionAttachment{Path: "   "})
	require.Error(t, err)

	// A path outside the three cache roots never resolves (ResolveCachedPath).
	err = svc.addAttachmentToZip(context.Background(), zw, 7, database.ReportSectionAttachment{Path: "etc/passwd"})
	require.Error(t, err)
}
