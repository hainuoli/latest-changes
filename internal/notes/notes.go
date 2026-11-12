// Package notes implements deterministic release-note updates derived from
// tiangolo/latest-changes-app. See LICENSE and UPSTREAM.md.
package notes

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hainuoli/latest-changes/internal/model"
)

const (
	DefaultDocument    = "# Release Notes\n\n## Latest Changes\n"
	DefaultCommitTitle = "📝 Update release notes"
)

// Section maps a category label to its release-note heading.
type Section struct {
	Label   string
	Heading string
}

// Sections defines precedence as well as document ordering.
var Sections = [...]Section{
	{"breaking", "Breaking Changes"},
	{"security", "Security Fixes"},
	{"feature", "Features"},
	{"bug", "Fixes"},
	{"refactor", "Refactors"},
	{"upgrade", "Upgrades"},
	{"docs", "Docs"},
	{"lang-all", "Translations"},
	{"infra", "Infrastructure"},
	{"internal", "Internal"},
}

// IsCategory reports whether name identifies a release-note section.
func IsCategory(name string) bool {
	for _, section := range Sections {
		if section.Label == name {
			return true
		}
	}
	return false
}

// RecognizedLabels returns distinct categories in their precedence order.
func RecognizedLabels(labels []model.Label) []string {
	set := map[string]bool{}
	for _, label := range labels {
		set[label.Name] = true
	}
	var result []string
	for _, section := range Sections {
		if set[section.Label] {
			result = append(result, section.Label)
		}
	}
	if set["release"] {
		result = append(result, "release")
	}
	return result
}

// ClassifyLabels validates that exactly one supported category is selected.
func ClassifyLabels(labels []model.Label) (state, description string) {
	matching := RecognizedLabels(labels)
	switch len(matching) {
	case 0:
		return "pending", "Waiting for one Latest Changes label"
	case 1:
		return "success", "Latest Changes label: " + matching[0]
	default:
		return "failure", "Multiple Latest Changes labels: " + strings.Join(matching, ", ")
	}
}

// GenerateNotes adds one PR entry while preserving historical releases.
func GenerateNotes(content string, pr model.PullRequest) (string, error) {
	matching := RecognizedLabels(pr.Labels)
	if len(matching) != 1 || matching[0] == "release" {
		return "", errors.New("expected exactly one release-note category")
	}
	message := fmt.Sprintf("* %s. PR [#%d](%s) by [@%s](%s).", pr.Title, pr.Number, pr.URL, pr.User.Login, pr.User.URL)
	// Match the upstream idempotency behavior across the complete document,
	// including entries already moved to historical releases.
	if strings.Contains(content, message) {
		return content, nil
	}
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if line == "## Latest Changes" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("release notes have no Latest Changes heading")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	current := trimBlank(lines[start+1 : end])
	byHeading := map[string]string{}
	for _, section := range Sections {
		byHeading["### "+section.Heading] = section.Label
	}
	sections := map[string][]string{}
	var leading []string
	selected := ""
	for _, line := range current {
		if strings.HasPrefix(line, "### ") {
			label, ok := byHeading[line]
			if !ok {
				return "", errors.New("unsupported Latest Changes section")
			}
			if _, duplicate := sections[label]; duplicate {
				return "", errors.New("duplicate Latest Changes section")
			}
			sections[label] = []string{}
			selected = label
		} else if selected == "" {
			leading = append(leading, line)
		} else {
			sections[selected] = append(sections[selected], line)
		}
	}
	for label, body := range sections {
		sections[label] = trimBlank(body)
	}
	sections[matching[0]] = append([]string{message}, sections[matching[0]]...)
	body := append([]string{}, trimBlank(leading)...)
	for _, section := range Sections {
		entries := sections[section.Label]
		if len(entries) == 0 {
			continue
		}
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, "### "+section.Heading, "")
		body = append(body, entries...)
	}
	result := append([]string{}, lines[:start+1]...)
	result = append(result, "")
	result = append(result, body...)
	result = append(result, "")
	result = append(result, lines[end:]...)
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}
	return strings.Join(result, "\n") + "\n", nil
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
