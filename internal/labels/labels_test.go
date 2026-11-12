package labels

import (
	"strings"
	"testing"

	"github.com/hainuoli/latest-changes/internal/model"
)

func TestSelectCandidate(t *testing.T) {
	cfg, err := ParseConfig([]byte(`auto-labels:
  docs:
    - exclude: docs/generated/**
    - docs/**
    - README*.md
  internal:
    - .github/**
  upgrade:
    - go.mod
    - go.sum
commit-title: "📝 Refresh release notes"
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		files []model.ChangedFile
		want  string
	}{
		{"nested", []model.ChangedFile{{Name: "docs/en/deep/index.md"}}, "docs"},
		{"readme", []model.ChangedFile{{Name: "README.zh-CN.md"}}, "docs"},
		{"hidden", []model.ChangedFile{{Name: ".github/workflows/go.yml"}}, "internal"},
		{"exclude", []model.ChangedFile{{Name: "docs/generated/schema.json"}}, ""},
		{"mixed-source", []model.ChangedFile{{Name: "README.md"}, {Name: "main.go"}}, ""},
		{"priority", []model.ChangedFile{{Name: "README.md"}, {Name: "go.mod"}}, "upgrade"},
		{"rename-source", []model.ChangedFile{{
			Name:         "docs/old.md",
			PreviousName: "main.go",
		}}, ""},
		{
			"rename-docs",
			[]model.ChangedFile{{
				Name:         "docs/new.md",
				PreviousName: "docs/old.md",
			}},
			"docs",
		},
		{"empty", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.SelectCandidate(tc.files); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	for _, input := range []string{
		"[", "", "auto-labels: []", "auto-labels: null", "auto-labels: {}\nunknown: true", "auto-labels: {}\n---\nauto-labels: {}",
		"auto-labels:\n  docs: []", "auto-labels:\n  unknown: [docs/**]", "auto-labels:\n  release: [docs/**]",
		"auto-labels:\n  docs: [42]", "auto-labels:\n  docs: ['']", "auto-labels:\n  docs: ['[']", "auto-labels:\n  docs: [{exclude: docs/**, extra: true}]",
		"auto-labels: {}\ncommit-title: |\n  📝 Update notes\n\n  Body",
		"auto-labels: {}\nauto-labels: {}", strings.Repeat(" ", 100001),
	} {
		t.Run(input[:min(len(input), 60)], func(t *testing.T) {
			if _, err := ParseConfig([]byte(input)); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

func TestValidateCommitTitle(t *testing.T) {
	for _, tc := range []struct {
		title string
		valid bool
	}{
		{"📝 Update release notes", true}, {"⬆️ Bump dependencies", true}, {"♻ Refactor service", true},
		{"✨ " + strings.Repeat("A", 70), true}, {"✨ " + strings.Repeat("A", 71), false},
		{"📝 Update notes\n\n[skip ci]", false}, {"📝 Update\tnotes", false}, {"📝 Update notes\u2028Body", false},
		{"📝 update notes", false}, {"📝 Update notes.", false}, {"📝 Update  notes", false}, {"📝 Update notes ", false},
		{"中文 Update notes", false}, {"feat: Update notes", false}, {"📝 (docs): Update notes", false}, {"", false},
	} {
		if err := ValidateCommitTitle(tc.title); (err == nil) != tc.valid {
			t.Errorf("%q: %v", tc.title, err)
		}
	}
}

func TestSelectionPreservesManualAndRelease(t *testing.T) {
	for _, tc := range []struct {
		existing  []model.Label
		explicit  string
		candidate string
		want      string
	}{
		{[]model.Label{{Name: "bug"}}, "bug", "docs", "bug"},
		{[]model.Label{{Name: "docs"}}, "", "bug", "bug"},
		{[]model.Label{{Name: "bug"}}, "", "docs", "bug"},
		{[]model.Label{{Name: "release"}, {Name: "bug"}}, "", "feature", "release"},
		{nil, "", "docs", "docs"},
	} {
		if got := SelectLabel(tc.existing, tc.explicit, tc.candidate); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}
