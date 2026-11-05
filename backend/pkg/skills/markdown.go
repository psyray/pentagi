package skills

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/vxcontrol/langchaingo/textsplitter"
)

const (
	defaultChunkSize    = 2000
	defaultChunkOverlap = 200
	maxTitleLen         = 200
	maxDescriptionLen   = 500
	maxSlugLen          = 80
)

// looseBool accepts a YAML boolean or its scalar spelling, so an engagement can
// write "anonymized: true" or "anonymized: yes" without failing the import.
type looseBool struct {
	Value bool
	Set   bool
}

func (b *looseBool) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected a boolean, got %s", value.Tag)
	}

	var text string
	if err := value.Decode(&text); err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(text)) {
	case "true", "yes", "on", "1":
		b.Value, b.Set = true, true
	case "false", "no", "off", "0", "":
		b.Value, b.Set = false, true
	default:
		return fmt.Errorf("expected a boolean, got %q", text)
	}

	return nil
}

// stringList accepts a YAML scalar or a sequence, so a pack can write
// "platform: HTB" or "platform: [HTB, Windows]" without failing the import.
type stringList []string

func (l *stringList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var single string
		if err := value.Decode(&single); err != nil {
			return err
		}
		single = strings.TrimSpace(single)
		if single == "" {
			*l = nil
			return nil
		}
		*l = stringList{single}
		return nil
	}

	var many []string
	if err := value.Decode(&many); err != nil {
		return err
	}
	*l = many
	return nil
}

type frontMatter struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Version     string     `yaml:"version"`
	Category    string     `yaml:"category"`
	Platform    stringList `yaml:"platform"`
	Tags        []string   `yaml:"tags"`
	Safety      string     `yaml:"safety"`
	License     string     `yaml:"license"`
	Source      string     `yaml:"source"`
	Visibility  string     `yaml:"visibility"`
	Status      string     `yaml:"status"`
	Language    string     `yaml:"language"`
	Kind        string     `yaml:"kind"`

	// Engagement fields. A reference or a skill may leave them empty; an
	// engagement uses them as its service fingerprint.
	Machine    string     `yaml:"machine"`
	OS         string     `yaml:"os"`
	Services   stringList `yaml:"services"`
	CVEs       stringList `yaml:"cves"`
	Outcome    string     `yaml:"outcome"`
	Anonymized looseBool  `yaml:"anonymized"`
}

// parseMarkdown separates a YAML frontmatter block from the body. A document
// that opens with '---' but never closes it is treated as plain markdown: a
// malformed import must not disappear behind an error.
func parseMarkdown(content string) (frontMatter, string, bool, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	if !strings.HasPrefix(content, "---\n") {
		return frontMatter{}, content, false, nil
	}

	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontMatter{}, content, false, nil
	}

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return frontMatter{}, content, false, fmt.Errorf("parse skill frontmatter: %w", err)
	}

	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	return fm, body, true, nil
}

// slugify produces a stable, human-readable identifier for a skill.
func slugify(value string) string {
	var b strings.Builder
	lastDash := false

	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() > 0:
			b.WriteByte('-')
			lastDash = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if utf8.RuneCountInString(slug) > maxSlugLen {
		runes := []rune(slug)
		slug = strings.Trim(string(runes[:maxSlugLen]), "-")
	}
	if slug == "" {
		return "untitled"
	}
	return slug
}

func firstHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func firstParagraph(content string) string {
	var lines []string
	started := false

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "---") {
			continue
		}
		started = true
		lines = append(lines, line)
	}

	return strings.Join(lines, " ")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func trimTitle(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if value == "" {
		value = "Untitled skill"
	}
	return truncateRunes(value, maxTitleLen)
}

func trimDescription(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	return truncateRunes(value, maxDescriptionLen)
}

func uniqueStrings(values ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range values {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func splitContent(content string, chunkSize, overlap int) ([]string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	if overlap < 0 {
		overlap = defaultChunkOverlap
	}

	splitter := textsplitter.NewRecursiveCharacter(
		textsplitter.WithChunkSize(chunkSize),
		textsplitter.WithChunkOverlap(overlap),
		textsplitter.WithCodeBlocks(true),
		textsplitter.WithHeadingHierarchy(true),
	)

	chunks, err := splitter.SplitText(content)
	if err != nil {
		return nil, fmt.Errorf("split skill content: %w", err)
	}

	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		out = append(out, chunk)
	}
	if len(out) == 0 {
		out = append(out, content)
	}
	return out, nil
}

type heading struct {
	level int
	text  string
}

func updateHeadings(text string, stack []heading) []heading {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}

		level := 0
		for level < len(line) && line[level] == '#' {
			level++
		}
		if level == 0 || level > 6 {
			continue
		}
		title := strings.TrimSpace(line[level:])
		if title == "" {
			continue
		}

		if level <= len(stack) {
			stack = stack[:level-1]
		}
		stack = append(stack, heading{level: level, text: title})
	}
	return stack
}

func renderHeadingPath(stack []heading) string {
	parts := make([]string, 0, len(stack))
	for _, h := range stack {
		parts = append(parts, h.text)
	}
	return strings.Join(parts, " > ")
}

// headingPaths attributes every chunk to the markdown heading that introduces
// it. The splitter preserves headings, so walking the source in chunk order is
// enough and does not need character offsets from the splitter.
func headingPaths(content string, chunks []string) []string {
	paths := make([]string, len(chunks))
	var stack []heading
	cursor := 0

	for i, chunk := range chunks {
		idx := strings.Index(content[cursor:], chunk)
		start := cursor
		if idx >= 0 {
			start = cursor + idx
		}

		stack = updateHeadings(content[cursor:start], stack)
		paths[i] = renderHeadingPath(stack)
		stack = updateHeadings(chunk, stack)

		if idx >= 0 {
			cursor = start + len(chunk)
		}
	}

	return paths
}

// Sections returns the markdown sections and their bodies. A section runs
// until the next heading of the same or a higher level.
func Sections(content string) []Section {
	type mark struct {
		line    int
		level   int
		heading string
		offset  int
	}

	lines := strings.Split(content, "\n")
	offsets := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		offsets[i] = offset
		offset += len(line) + 1
	}

	var marks []mark
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level > 6 {
			continue
		}
		heading := strings.TrimSpace(trimmed[level:])
		if heading == "" {
			continue
		}
		marks = append(marks, mark{line: i, level: level, heading: heading, offset: offsets[i]})
	}

	sections := make([]Section, 0, len(marks))
	for i, current := range marks {
		endOffset := len(content)
		for j := i + 1; j < len(marks); j++ {
			if marks[j].level <= current.level {
				endOffset = marks[j].offset
				break
			}
		}
		if endOffset < current.offset {
			endOffset = current.offset
		}
		sections = append(sections, Section{
			Heading: current.heading,
			Level:   current.level,
			Content: strings.TrimSpace(content[current.offset:endOffset]),
			Offset:  current.offset,
		})
	}

	return sections
}

// ExtractSection returns the body of the first section whose heading matches,
// case-insensitively. It accepts a full heading or its path suffix.
func ExtractSection(content, name string) (string, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "", false
	}

	for _, section := range Sections(content) {
		heading := strings.ToLower(section.Heading)
		if heading == name || strings.HasSuffix(heading, " > "+name) || strings.Contains(heading, name) {
			return section.Content, true
		}
	}

	return "", false
}

func embeddingText(title, headingPath, chunk string, maxBytes int) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(title))
	if headingPath != "" {
		b.WriteString("\n")
		b.WriteString(headingPath)
	}
	b.WriteString("\n\n")
	b.WriteString(chunk)

	text := b.String()
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}

	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
