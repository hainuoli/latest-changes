package labels

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
	"go.yaml.in/yaml/v3"
)

type (
	// Config contains path rules and the single-line automation commit title.
	Config struct {
		AutoLabels  map[string][]Rule `yaml:"auto-labels"`
		CommitTitle string            `yaml:"commit-title"`
	}

	// Rule includes or excludes a path using first-match precedence.
	Rule struct {
		Pattern string
		Exclude bool
	}
)

// UnmarshalYAML decodes an inclusion pattern or an explicit exclusion.
func (r *Rule) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return errors.New("path rules must be strings")
		}
		r.Pattern = node.Value
	case yaml.MappingNode:
		if len(node.Content) != 2 || node.Content[0].Value != "exclude" || node.Content[1].Tag != "!!str" {
			return errors.New("invalid exclusion rule")
		}
		r.Pattern, r.Exclude = node.Content[1].Value, true
	default:
		return errors.New("invalid path rule")
	}
	r.Pattern = strings.TrimSpace(r.Pattern)
	if r.Pattern == "" || strings.HasPrefix(r.Pattern, "/") || !doublestar.ValidatePattern(r.Pattern) {
		return errors.New("invalid path pattern")
	}
	return nil
}

// ValidateCommitTitle rejects unsupported prefixes, bodies, and malformed subjects.
func ValidateCommitTitle(title string) error {
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) > 72 || strings.TrimSpace(title) != title || strings.Contains(title, "  ") {
		return errors.New("commit-title must be a single Gitmoji title of at most 72 characters")
	}
	for _, r := range title {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("commit-title cannot contain a body or control characters")
		}
	}
	emoji, subject, ok := strings.Cut(title, " ")
	emoji = strings.TrimSuffix(emoji, "\ufe0f")
	allowed := "|✨|🐛|💥|🔒|📝|✏|🌐|⬆|⬇|♻|🎨|⚡|🔥|👷|🔧|🔨|🚨|✅|🔖|🎉|📄|🙈|"
	if !ok || !strings.Contains(allowed, "|"+emoji+"|") || subject == "" || subject[0] < 'A' || subject[0] > 'Z' || strings.HasSuffix(title, ".") {
		return errors.New("commit-title requires a supported Gitmoji and capitalized subject without a trailing period")
	}
	return nil
}

// ParseConfig validates a single repository configuration document.
func ParseConfig(data []byte) (Config, error) {
	if len(data) > 100_000 {
		return Config{}, errors.New("configuration too large")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("expected one YAML document")
	}
	if cfg.AutoLabels == nil {
		return Config{}, errors.New("auto-labels must be a mapping")
	}
	for label, rules := range cfg.AutoLabels {
		if !notes.IsCategory(label) || len(rules) == 0 {
			return Config{}, errors.New("invalid auto-label category or empty rules")
		}
	}
	if cfg.CommitTitle == "" {
		cfg.CommitTitle = notes.DefaultCommitTitle
	}
	if err := ValidateCommitTitle(cfg.CommitTitle); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SelectCandidate classifies every changed path and returns the highest-priority category.
func (c Config) SelectCandidate(files []model.ChangedFile) string {
	candidates := map[string]bool{}
	for _, file := range files {
		paths := []string{file.Name}
		if file.PreviousName != "" && file.PreviousName != file.Name {
			paths = append(paths, file.PreviousName)
		}
		for _, path := range paths {
			classified := false
			for label, rules := range c.AutoLabels {
				for _, rule := range rules {
					matched, _ := doublestar.Match(rule.Pattern, path)
					if !matched {
						continue
					}
					if !rule.Exclude {
						candidates[label], classified = true, true
					}
					break
				}
			}
			if !classified {
				return ""
			}
		}
	}
	for _, section := range notes.Sections {
		if candidates[section.Label] {
			return section.Label
		}
	}
	return ""
}

// SelectLabel preserves manual choices and otherwise applies category precedence.
func SelectLabel(existing []model.Label, explicit, candidate string) string {
	matching := notes.RecognizedLabels(existing)
	if len(matching) == 1 && matching[0] == explicit {
		return explicit
	}
	set := map[string]bool{}
	for _, label := range matching {
		if label == "release" {
			return "release"
		}
		set[label] = true
	}
	if candidate != "" {
		set[candidate] = true
	}
	for _, section := range notes.Sections {
		if set[section.Label] {
			return section.Label
		}
	}
	return ""
}
