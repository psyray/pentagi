package services

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zipEntryNames(t *testing.T, archive []byte) []string {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)

	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	return names
}

// nmap -oA /work/results/nmap_allports writes three files and no file at the base
// name itself. A writer citing that base name must still reach the archive with
// the three siblings bundled (plan § 6.10 refinement).
func TestReportArchive_BundlesSiblingsForAnExtensionLessCitation(t *testing.T) {
	const (
		cited = "/work/results/nmap_allports"
		dir   = "/work/results"
	)

	dockerClient := &fakeDockerClient{
		copyFromErrMap: map[string]error{
			cited: errors.New("Could not find the file " + cited),
		},
		copyFromBodyMap: map[string][]byte{
			dir + "/nmap_allports.nmap":  buildContainerTar([]tarTestEntry{{content: "nmap", name: "nmap_allports.nmap"}}),
			dir + "/nmap_allports.gnmap": buildContainerTar([]tarTestEntry{{content: "gnmap", name: "nmap_allports.gnmap"}}),
			dir + "/nmap_allports.xml":   buildContainerTar([]tarTestEntry{{content: "xml", name: "nmap_allports.xml"}}),
		},
		listDirMap: map[string][]container.PathStat{
			dir: {
				{Mode: 0o644, Name: "nmap_allports.nmap"},
				{Mode: 0o644, Name: "nmap_allports.gnmap"},
				{Mode: 0o644, Name: "nmap_allports.xml"},
				// A different file and a directory must not be dragged in.
				{Mode: 0o644, Name: "other.txt"},
				{Mode: os.ModeDir | 0o755, Name: "archive_src"},
			},
		},
	}

	svc := &ReportArchiveService{dockerClient: dockerClient}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	count, err := svc.addCitedFile(context.Background(), zw, "pentagi-terminal-1", cited, map[string]struct{}{})
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.NoError(t, zw.Close())

	assert.ElementsMatch(t, []string{
		"annexes/files/work/results/nmap_allports.nmap",
		"annexes/files/work/results/nmap_allports.gnmap",
		"annexes/files/work/results/nmap_allports.xml",
	}, zipEntryNames(t, buf.Bytes()))
}

// A cited file that exists is bundled directly, without listing its directory.
func TestReportArchive_BundlesAnExistingCitationDirectly(t *testing.T) {
	const cited = "/work/results/user.txt"

	dockerClient := &fakeDockerClient{
		copyFromBodyMap: map[string][]byte{
			cited: buildContainerTar([]tarTestEntry{{content: "uid=0", name: "user.txt"}}),
		},
		listDirErr: errors.New("the directory must not be listed when the file exists"),
	}

	svc := &ReportArchiveService{dockerClient: dockerClient}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	count, err := svc.addCitedFile(context.Background(), zw, "pentagi-terminal-1", cited, map[string]struct{}{})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, zw.Close())

	assert.Equal(t, []string{"annexes/files/work/results/user.txt"}, zipEntryNames(t, buf.Bytes()))
}

// When neither the cited path nor any sibling exists, the original copy error is
// surfaced so MISSING.txt still names the file the writer pointed at.
func TestReportArchive_KeepsTheCopyErrorWhenNoSiblingExists(t *testing.T) {
	const (
		cited = "/work/results/nmap_allports"
		dir   = "/work/results"
	)

	dockerClient := &fakeDockerClient{
		copyFromErrMap: map[string]error{
			cited: errors.New("Could not find the file " + cited),
		},
		listDirMap: map[string][]container.PathStat{
			dir: {{Mode: 0o644, Name: "unrelated.txt"}},
		},
	}

	svc := &ReportArchiveService{dockerClient: dockerClient}
	zw := zip.NewWriter(&bytes.Buffer{})

	_, err := svc.addCitedFile(context.Background(), zw, "pentagi-terminal-1", cited, map[string]struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not find the file")
}

// A sibling another ref already bundled is not written twice, and a repeated
// citation is a no-op rather than a second MISSING line.
func TestReportArchive_BundlesEachCitationOnce(t *testing.T) {
	const (
		cited = "/work/results/nmap_allports"
		dir   = "/work/results"
	)

	dockerClient := &fakeDockerClient{
		copyFromErrMap: map[string]error{
			cited: errors.New("Could not find the file " + cited),
		},
		copyFromBodyMap: map[string][]byte{
			dir + "/nmap_allports.nmap": buildContainerTar([]tarTestEntry{{content: "nmap", name: "nmap_allports.nmap"}}),
		},
		listDirMap: map[string][]container.PathStat{
			dir: {{Mode: 0o644, Name: "nmap_allports.nmap"}},
		},
	}

	svc := &ReportArchiveService{dockerClient: dockerClient}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	bundled := map[string]struct{}{}

	first, err := svc.addCitedFile(context.Background(), zw, "pentagi-terminal-1", cited, bundled)
	require.NoError(t, err)
	assert.Equal(t, 1, first)

	second, err := svc.addCitedFile(context.Background(), zw, "pentagi-terminal-1", cited, bundled)
	require.NoError(t, err)
	assert.Equal(t, 0, second)

	require.NoError(t, zw.Close())
	assert.Equal(t, []string{"annexes/files/work/results/nmap_allports.nmap"}, zipEntryNames(t, buf.Bytes()))
}
