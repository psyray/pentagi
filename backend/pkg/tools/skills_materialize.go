package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"pentagi/pkg/docker"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/resources"
	"pentagi/pkg/skills"

	"github.com/moby/moby/client"
)

const (
	// maxMaterializeFiles and maxMaterializeBytes bound what a single call can
	// copy into a flow container. Attachments already come through the upload
	// API, so these caps only protect the container filesystem.
	maxMaterializeFiles = 100
	maxMaterializeBytes = 128 << 20

	// skillManifestName is written next to the attachments so the directory is
	// self-describing when the agent lists it later in the flow.
	skillManifestName = "SKILL.md"
)

// skillMaterializer copies the attachments of one skill into the skills
// directory of a flow, both in the flow data cache and in the running
// container. A nil docker client is allowed: the files are still written to
// the cache and pushed when the container starts.
type skillMaterializer struct {
	dataDir       string
	flowID        int64
	containerName string
	docker        docker.DockerClient
}

type skillMaterializeResult struct {
	Slug       string
	Directory  string
	Files      []string
	Skipped    []string
	Pushed     bool
	TotalBytes int64
}

func newSkillMaterializer(dataDir string, flowID int64, containerName string, dc docker.DockerClient) *skillMaterializer {
	return &skillMaterializer{
		dataDir:       dataDir,
		flowID:        flowID,
		containerName: containerName,
		docker:        dc,
	}
}

// materialize writes the manifest and the requested attachments under
// skills/<slug>/ and pushes them into the container when it is running.
// Existing files are kept unless overwrite is set, so an agent that edited a
// script does not lose its changes to a second call.
func (m *skillMaterializer) materialize(
	ctx context.Context,
	doc *skills.Document,
	files []skills.File,
	requested []string,
	overwrite bool,
) (*skillMaterializeResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("skill not found")
	}
	if m.dataDir == "" {
		return nil, fmt.Errorf("skills cannot be materialized: the data directory is not configured")
	}
	dataDir, err := filepath.Abs(m.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the data directory: %w", err)
	}

	slug, err := resources.SanitizeResourceFileName(doc.Slug)
	if err != nil {
		return nil, fmt.Errorf("invalid skill slug: %w", err)
	}

	selected, err := selectSkillFiles(files, requested)
	if err != nil {
		return nil, err
	}
	if len(selected) > maxMaterializeFiles {
		return nil, fmt.Errorf("%d attachments exceed the limit of %d per materialization",
			len(selected), maxMaterializeFiles)
	}

	var declared int64
	for _, file := range selected {
		declared += file.Size
	}
	if declared > maxMaterializeBytes {
		return nil, fmt.Errorf("attachments total %d bytes, above the limit of %d", declared, maxMaterializeBytes)
	}

	dir := filepath.Join(flowfiles.FlowSkillsDir(dataDir, uint64(m.flowID)), slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create the skill directory: %w", err)
	}

	result := &skillMaterializeResult{
		Slug:      slug,
		Directory: path.Join(flowfiles.SkillsDirName, slug),
	}
	entries := make([]flowfiles.TarEntry, 0, len(selected)+1)

	manifestPath := filepath.Join(dir, skillManifestName)
	written, err := writeSkillFile(manifestPath, []byte(doc.Content), overwrite)
	if err != nil {
		return nil, err
	}
	if written {
		entries = append(entries, flowfiles.TarEntry{
			LocalPath: manifestPath,
			TarPath:   path.Join(flowfiles.SkillsDirName, slug, skillManifestName),
		})
	}

	for _, file := range selected {
		relPath := path.Clean(strings.ReplaceAll(file.Path, "\\", "/"))
		dst := filepath.Join(dir, filepath.FromSlash(relPath))
		if !flowfiles.IsWithinDir(dst, dir) {
			return nil, fmt.Errorf("attachment %q escapes the skill directory", file.Path)
		}

		written, size, err := copySkillBlob(dataDir, file, dst, overwrite)
		if err != nil {
			return nil, err
		}
		if !written {
			result.Skipped = append(result.Skipped, relPath)
			continue
		}

		result.Files = append(result.Files, relPath)
		result.TotalBytes += size
		entries = append(entries, flowfiles.TarEntry{
			LocalPath: dst,
			TarPath:   path.Join(flowfiles.SkillsDirName, slug, relPath),
		})
	}

	if len(entries) == 0 {
		return result, nil
	}

	pushed, err := m.push(ctx, entries)
	if err != nil {
		return result, err
	}
	result.Pushed = pushed

	return result, nil
}

// push copies the written files into the running primary container. When the
// container is absent the files stay in the flow cache and are synced when the
// container next starts.
func (m *skillMaterializer) push(ctx context.Context, entries []flowfiles.TarEntry) (bool, error) {
	if m.docker == nil || m.containerName == "" {
		return false, nil
	}

	running, err := m.docker.IsContainerRunning(ctx, m.containerName)
	if err != nil {
		return false, fmt.Errorf("failed to check the flow container: %w", err)
	}
	if !running {
		return false, nil
	}

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- flowfiles.WriteFilesTar(pw, entries)
	}()

	copyErr := m.docker.CopyToContainer(ctx, m.containerName, docker.WorkFolderPathInContainer, pr,
		client.CopyToContainerOptions{AllowOverwriteDirWithFile: true})
	pr.Close()
	writeErr := <-errCh

	if copyErr != nil {
		return false, fmt.Errorf("failed to copy skill files into the container: %w", copyErr)
	}
	if writeErr != nil {
		return false, fmt.Errorf("failed to build the skill archive: %w", writeErr)
	}

	return true, nil
}

// selectSkillFiles keeps the requested paths only, preserving the store order.
func selectSkillFiles(files []skills.File, requested []string) ([]skills.File, error) {
	if len(requested) == 0 {
		return files, nil
	}

	wanted := make(map[string]struct{}, len(requested))
	for _, item := range requested {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		cleaned := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
		if _, err := resources.SanitizeResourcePath(cleaned); err != nil {
			return nil, fmt.Errorf("invalid attachment path %q: %w", item, err)
		}
		wanted[cleaned] = struct{}{}
	}

	selected := make([]skills.File, 0, len(wanted))
	for _, file := range files {
		key := path.Clean(strings.ReplaceAll(file.Path, "\\", "/"))
		if _, ok := wanted[key]; ok {
			selected = append(selected, file)
			delete(wanted, key)
		}
	}

	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for key := range wanted {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown attachment(s): %s", strings.Join(missing, ", "))
	}

	return selected, nil
}

// copySkillBlob writes one attachment from the resource blob store to dst.
func copySkillBlob(dataDir string, file skills.File, dst string, overwrite bool) (bool, int64, error) {
	if !resources.IsValidBlobHash(file.BlobRef) {
		return false, 0, fmt.Errorf("attachment %q has an invalid blob reference", file.Path)
	}

	src := resources.BlobPath(dataDir, file.BlobRef)
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, 0, fmt.Errorf("attachment %q is no longer stored: %w", file.Path, err)
	}

	if _, err := os.Stat(dst); err == nil && !overwrite {
		return false, 0, nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return false, 0, fmt.Errorf("failed to create the directory for %q: %w", file.Path, err)
	}
	if err := copyFileContents(src, dst); err != nil {
		return false, 0, fmt.Errorf("failed to materialize %q: %w", file.Path, err)
	}

	return true, srcInfo.Size(), nil
}

func writeSkillFile(dst string, content []byte, overwrite bool) (bool, error) {
	if _, err := os.Stat(dst); err == nil && !overwrite {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return false, fmt.Errorf("failed to create the skill directory: %w", err)
	}
	if err := os.WriteFile(dst, content, 0644); err != nil {
		return false, fmt.Errorf("failed to write %s: %w", skillManifestName, err)
	}
	return true, nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}
