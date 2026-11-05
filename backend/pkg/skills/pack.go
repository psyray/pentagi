package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// PackManifestFile is the manifest at the root of a pack tree.
	PackManifestFile = "pack.yaml"
	// PackSkillsDir holds one directory per skill, each with a SKILL.md.
	PackSkillsDir = "skills"
	// PackReferencesDir holds one file or one directory per reference.
	PackReferencesDir = "references"
	// PackEngagementsDir holds the engagement write-ups.
	PackEngagementsDir = "engagements"
	// PackSkillFile is the required body of a skill directory.
	PackSkillFile = "SKILL.md"
	// PackAttachmentsDir holds the attachments of a skill.
	PackAttachmentsDir = "files"

	maxPackSourceBytes = 4 << 20
	maxPackFiles       = 500
)

// PackManifest is the tree-format pack.yaml. The three lists act as an
// allowlist when present: anything not listed is left out, which lets a pack
// ship drafts it does not want imported yet.
type PackManifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Version     string   `yaml:"version,omitempty"`
	License     string   `yaml:"license,omitempty"`
	Visibility  string   `yaml:"visibility,omitempty"`
	Status      string   `yaml:"status,omitempty"`
	Skills      []string `yaml:"skills,omitempty"`
	References  []string `yaml:"references,omitempty"`
	Engagements []string `yaml:"engagements,omitempty"`
}

// PackFile is one attachment read from a skill directory.
type PackFile struct {
	// Path is stored as is: "files/<relative path>" under the skill.
	Path    string
	Content []byte
}

// PackDocument is one document read from a pack tree, ready to import.
type PackDocument struct {
	Kind       string
	Slug       string
	Title      string
	Markdown   string
	SourcePath string
	Files      []PackFile
}

// Pack is a parsed pack tree.
type Pack struct {
	Manifest  PackManifest
	Documents []PackDocument
	Warnings  []string
}

// PackExport is an in-memory pack tree, ready to be written to a directory or
// to a zip archive.
type PackExport struct {
	Manifest PackManifest
	Files    map[string][]byte
}

// ManifestIsTree reports whether a pack.yaml uses the tree format. The previous
// format lists explicit documents and is still accepted by the CLI.
func ManifestIsTree(data []byte) bool {
	var probe struct {
		Skills      []string `yaml:"skills"`
		References  []string `yaml:"references"`
		Engagements []string `yaml:"engagements"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}

	return len(probe.Skills) > 0 || len(probe.References) > 0 || len(probe.Engagements) > 0
}

// ParsePackManifest reads the tree manifest and checks the fields the import
// cannot invent.
func ParsePackManifest(data []byte) (*PackManifest, error) {
	var manifest PackManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("skills: parse %s: %w", PackManifestFile, err)
	}

	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name == "" {
		return nil, fmt.Errorf("skills: %s has no name", PackManifestFile)
	}
	manifest.Visibility = strings.TrimSpace(manifest.Visibility)
	if manifest.Visibility != "" && !validVisibility(manifest.Visibility) {
		return nil, fmt.Errorf("skills: %s has an unknown visibility %q", PackManifestFile, manifest.Visibility)
	}
	manifest.Status = strings.TrimSpace(manifest.Status)
	if manifest.Status != "" && !validStatus(manifest.Status) {
		return nil, fmt.Errorf("skills: %s has an unknown status %q", PackManifestFile, manifest.Status)
	}

	return &manifest, nil
}

// ParseTreePack reads a pack tree from any filesystem: os.DirFS for a directory,
// or a zip reader wrapped in fs.FS.
func ParseTreePack(fsys fs.FS) (*Pack, error) {
	data, err := fs.ReadFile(fsys, PackManifestFile)
	if err != nil {
		return nil, fmt.Errorf("skills: read %s: %w", PackManifestFile, err)
	}

	manifest, err := ParsePackManifest(data)
	if err != nil {
		return nil, err
	}
	if !ManifestIsTree(data) {
		return nil, fmt.Errorf("skills: %s uses the legacy document format, not the tree format", PackManifestFile)
	}

	pack := &Pack{Manifest: *manifest}
	seen := map[string]struct{}{}

	pack.readSkills(fsys, seen)
	pack.readReferences(fsys, seen)
	pack.readEngagements(fsys, seen)

	if len(pack.Documents) == 0 {
		return nil, errors.New("skills: the pack contains no document")
	}

	return pack, nil
}

func (p *Pack) readSkills(fsys fs.FS, seen map[string]struct{}) {
	entries, err := fs.ReadDir(fsys, PackSkillsDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("cannot read %s: %v", PackSkillsDir, err))
		}
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		if !packAllows(p.Manifest.Skills, slug) {
			continue
		}
		if !p.claim(KindSkill, slug, seen) {
			continue
		}

		dir := path.Join(PackSkillsDir, slug)
		body, err := readPackFile(fsys, path.Join(dir, PackSkillFile))
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: no %s, skill skipped", dir, PackSkillFile))
			delete(seen, KindSkill+"/"+slug)
			continue
		}

		doc := PackDocument{
			Kind:       KindSkill,
			Slug:       slug,
			Title:      packFirstHeading(string(body), slug),
			Markdown:   string(body),
			SourcePath: path.Join(dir, PackSkillFile),
			Files:      p.readAttachments(fsys, path.Join(dir, PackAttachmentsDir)),
		}
		p.Documents = append(p.Documents, doc)
	}
}

func (p *Pack) readAttachments(fsys fs.FS, dir string) []PackFile {
	var files []PackFile

	_ = fs.WalkDir(fsys, dir, func(entryPath string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if len(files) >= maxPackFiles {
			return nil
		}
		content, readErr := readPackFile(fsys, entryPath)
		if readErr != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %v", entryPath, readErr))
			return nil
		}
		files = append(files, PackFile{
			Path:    path.Join(PackAttachmentsDir, strings.TrimPrefix(entryPath, dir+"/")),
			Content: content,
		})
		return nil
	})

	return files
}

func (p *Pack) readReferences(fsys fs.FS, seen map[string]struct{}) {
	entries, err := fs.ReadDir(fsys, PackReferencesDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		slug := strings.TrimSuffix(name, path.Ext(name))

		if entry.IsDir() {
			if !packAllows(p.Manifest.References, name) || !p.claim(KindReference, name, seen) {
				continue
			}
			doc, ok := p.readReferenceDir(fsys, path.Join(PackReferencesDir, name), name)
			if ok {
				p.Documents = append(p.Documents, doc)
			} else {
				delete(seen, KindReference+"/"+name)
			}
			continue
		}

		if !textPackFile(name) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s/%s: unsupported file, skipped", PackReferencesDir, name))
			continue
		}
		if !packAllows(p.Manifest.References, slug) || !p.claim(KindReference, slug, seen) {
			continue
		}

		content, err := readPackFile(fsys, path.Join(PackReferencesDir, name))
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s/%s: %v", PackReferencesDir, name, err))
			delete(seen, KindReference+"/"+slug)
			continue
		}

		p.Documents = append(p.Documents, PackDocument{
			Kind:       KindReference,
			Slug:       slug,
			Title:      packFirstHeading(string(content), slug),
			Markdown:   string(content),
			SourcePath: path.Join(PackReferencesDir, name),
		})
	}
}

// readReferenceDir turns a chapter directory into one document, so retrieval can
// cite a chapter heading instead of one file per chapter.
func (p *Pack) readReferenceDir(fsys fs.FS, dir, slug string) (PackDocument, bool) {
	var parts []string

	err := fs.WalkDir(fsys, dir, func(entryPath string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := path.Base(entryPath)
		if !textPackFile(name) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: unsupported file, skipped", entryPath))
			return nil
		}
		content, readErr := readPackFile(fsys, entryPath)
		if readErr != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %v", entryPath, readErr))
			return nil
		}
		body := strings.TrimSpace(string(content))
		if body == "" {
			return nil
		}
		if !strings.HasPrefix(body, "# ") {
			body = "# " + strings.TrimSuffix(name, path.Ext(name)) + "\n\n" + body
		}
		parts = append(parts, body)
		return nil
	})
	if err != nil || len(parts) == 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s: no readable chapter, reference skipped", dir))
		return PackDocument{}, false
	}

	markdown := strings.Join(parts, "\n\n")
	return PackDocument{
		Kind:       KindReference,
		Slug:       slug,
		Title:      packFirstHeading(markdown, slug),
		Markdown:   markdown,
		SourcePath: dir,
	}, true
}

func (p *Pack) readEngagements(fsys fs.FS, seen map[string]struct{}) {
	entries, err := fs.ReadDir(fsys, PackEngagementsDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("cannot read %s: %v", PackEngagementsDir, err))
		}
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !textPackFile(name) {
			continue
		}
		slug := strings.TrimSuffix(name, path.Ext(name))
		if !packAllows(p.Manifest.Engagements, slug) || !p.claim(KindEngagement, slug, seen) {
			continue
		}
		content, err := readPackFile(fsys, path.Join(PackEngagementsDir, name))
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s/%s: %v", PackEngagementsDir, name, err))
			delete(seen, KindEngagement+"/"+slug)
			continue
		}
		p.Documents = append(p.Documents, PackDocument{
			Kind:       KindEngagement,
			Slug:       slug,
			Title:      packFirstHeading(string(content), slug),
			Markdown:   string(content),
			SourcePath: path.Join(PackEngagementsDir, name),
		})
	}
}

func (p *Pack) claim(kind, slug string, seen map[string]struct{}) bool {
	if _, err := sanitizePackSlug(slug); err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%q: %v", slug, err))
		return false
	}
	key := kind + "/" + slug
	if _, exists := seen[key]; exists {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s: duplicate %s %q, skipped", kind, kind, slug))
		return false
	}
	seen[key] = struct{}{}
	return true
}

// RenderPack lays out documents and attachments as a pack tree. The manifest
// lists are derived from the documents, so an export can be re-imported as is.
func RenderPack(manifest PackManifest, documents []Document, files map[string][]PackFile) *PackExport {
	export := &PackExport{Manifest: manifest, Files: map[string][]byte{}}

	var skills, references, engagements []string

	for _, doc := range documents {
		switch doc.Kind {
		case KindSkill:
			skills = append(skills, doc.Slug)
			dir := path.Join(PackSkillsDir, doc.Slug)
			export.Files[path.Join(dir, PackSkillFile)] = []byte(doc.Content)
			for _, file := range files[doc.ID] {
				relative := strings.TrimPrefix(file.Path, PackAttachmentsDir+"/")
				export.Files[path.Join(dir, PackAttachmentsDir, relative)] = file.Content
			}
		case KindReference:
			references = append(references, doc.Slug)
			export.Files[path.Join(PackReferencesDir, doc.Slug+".md")] = []byte(doc.Content)
		case KindEngagement:
			engagements = append(engagements, doc.Slug)
			export.Files[path.Join(PackEngagementsDir, doc.Slug+".md")] = []byte(doc.Content)
		}
	}

	sort.Strings(skills)
	sort.Strings(references)
	sort.Strings(engagements)

	export.Manifest.Skills = skills
	export.Manifest.References = references
	export.Manifest.Engagements = engagements

	manifestYAML, err := yaml.Marshal(export.Manifest)
	if err != nil {
		// The manifest only holds strings and slices of strings, so a marshal
		// failure is impossible; keep the export usable regardless.
		manifestYAML = []byte("name: " + manifest.Name + "\n")
	}
	export.Files[PackManifestFile] = manifestYAML

	return export
}

func packAllows(list []string, name string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}

func textPackFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".txt", ".rst":
		return true
	default:
		return false
	}
}

func readPackFile(fsys fs.FS, name string) ([]byte, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPackSourceBytes {
		return nil, fmt.Errorf("%d bytes exceeds the limit of %d", info.Size(), maxPackSourceBytes)
	}
	return fs.ReadFile(fsys, name)
}

func sanitizePackSlug(slug string) (string, error) {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return "", errors.New("empty slug")
	}
	if strings.ContainsAny(trimmed, "/\\") || trimmed == "." || trimmed == ".." {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	return trimmed, nil
}

// packFirstHeading returns the first level-one heading of a markdown body, or
// the fallback when the body has none.
func packFirstHeading(markdown, fallback string) string {
	if heading := firstHeading(markdown); heading != "" {
		return heading
	}
	return fallback
}
