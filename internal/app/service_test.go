package app

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
)

type fakeGitHub struct {
	pr          model.PullRequest
	config      string
	content     string
	sha         string
	files       []model.ChangedFile
	fileErr     error
	fileReads   int
	status      string
	stateSHA    string
	description string
	added       []string
	removed     []string
	writes      int
	reads       int
	conflict    bool
	changeHead  bool
	message     string
}

func fixture() model.Event {
	count := 1
	return model.Event{
		Action: "opened",
		Repository: model.Repository{
			ID:            42,
			FullName:      "owner/repo",
			DefaultBranch: "main",
		},
		PullRequest: model.PullRequest{
			Number: 1,
			Title:  "✨ Add feature",
			URL:    "https://github.com/owner/repo/pull/1",
			Merged: true,
			User: model.User{
				Login: "contributor",
				URL:   "https://github.com/contributor",
			},
			Base: model.Ref{
				Name: "main",
				SHA:  "base",
			},
			Head:         model.Ref{SHA: "new-head"},
			ChangedFiles: &count,
		},
	}
}

func (f *fakeGitHub) GetPullRequest(context.Context, model.Repository, int) (model.PullRequest, error) {
	f.reads++
	pr := f.pr
	if f.changeHead && f.reads > 1 {
		pr.Head.SHA = "later-head"
	}
	return pr, nil
}

func (f *fakeGitHub) GetPullRequestFiles(context.Context, model.Repository, model.PullRequest) ([]model.ChangedFile, error) {
	f.fileReads++
	return f.files, f.fileErr
}

func (f *fakeGitHub) GetFile(_ context.Context, _ model.Repository, path, ref string) (*github.File, error) {
	content, sha := "", f.sha
	if path == configPath {
		content = f.config
		sha = "config-sha"
	} else if path == "docs/release-notes.md" {
		content = f.content
	}
	if content == "" {
		return nil, nil
	}
	return &github.File{
		Type:     "file",
		Path:     path,
		SHA:      sha,
		Size:     len(content),
		Encoding: "base64",
		Content:  base64.StdEncoding.EncodeToString([]byte(content)),
	}, nil
}

func (f *fakeGitHub) AddLabel(_ context.Context, _ model.Repository, _ int, label string) error {
	f.added = append(f.added, label)
	return nil
}

func (f *fakeGitHub) RemoveLabel(_ context.Context, _ model.Repository, _ int, label string) error {
	f.removed = append(f.removed, label)
	return nil
}

func (f *fakeGitHub) SetStatus(_ context.Context, _ model.Repository, sha, state, description string) error {
	f.stateSHA, f.status, f.description = sha, state, description
	return nil
}

func (f *fakeGitHub) UpdateFile(_ context.Context, _ model.Repository, path, sha, title, content string) error {
	f.writes++
	f.message = title
	if f.conflict && f.writes == 1 {
		f.sha = "concurrent-sha"
		f.content = notes.DefaultDocument + "\n### Docs\n\n* Concurrent change.\n"
		return &github.APIError{StatusCode: 409}
	}
	if f.content != "" && sha != f.sha {
		return errors.New("wrong SHA")
	}
	f.content, f.sha = content, "updated-sha"
	return nil
}

func TestLabelProcessing(t *testing.T) {
	config := "auto-labels:\n  docs: [docs/**]\n  internal: [.github/**]\n"
	for _, tc := range []struct {
		name       string
		action     string
		config     string
		existing   []model.Label
		files      []model.ChangedFile
		fileErr    error
		state      string
		added      string
		changeHead bool
	}{
		{
			name:   "automatic-docs",
			action: "opened",
			config: config,
			files:  []model.ChangedFile{{Name: "docs/index.md"}},
			state:  "success",
			added:  "docs",
		},
		{
			name:   "mixed-source",
			action: "opened",
			config: config,
			files:  []model.ChangedFile{{Name: "docs/index.md"}, {Name: "main.go"}},
			state:  "pending",
		},
		{
			name:     "manual",
			action:   "labeled",
			config:   config,
			existing: []model.Label{{Name: "bug"}},
			files:    []model.ChangedFile{{Name: "docs/index.md"}},
			state:    "success",
		},
		{
			name:     "unconfigured-multiple",
			action:   "opened",
			existing: []model.Label{{Name: "bug"}, {Name: "feature"}},
			state:    "failure",
		},
		{
			name:   "invalid-config",
			action: "opened",
			config: "auto-labels: []",
			state:  "failure",
		},
		{
			name:    "classification-error",
			action:  "opened",
			config:  config,
			fileErr: errors.New("API unavailable"),
			state:   "failure",
		},
		{
			name:       "new-head-during-work",
			action:     "opened",
			config:     config,
			files:      []model.ChangedFile{{Name: "docs/index.md"}},
			changeHead: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := fixture()
			event.Action = tc.action
			event.PullRequest.Head.SHA = "old-event-head"
			current := event.PullRequest
			current.Head.SHA = "new-head"
			current.Labels = tc.existing
			if tc.action == "labeled" {
				event.Label = &model.Label{Name: "bug"}
			}
			fake := &fakeGitHub{
				pr:         current,
				config:     tc.config,
				files:      tc.files,
				fileErr:    tc.fileErr,
				changeHead: tc.changeHead,
			}
			if err := (NewService(fake, fake)).ProcessEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			if fake.status != tc.state {
				t.Fatalf("got %q want %q (%s)", fake.status, tc.state, fake.description)
			}
			if tc.state != "" && fake.stateSHA != "new-head" {
				t.Fatal("status written to stale event SHA")
			}
			if tc.added != "" && (len(fake.added) != 1 || fake.added[0] != tc.added) {
				t.Fatal(fake.added)
			}
			if tc.name == "manual" && (len(fake.added) > 0 || len(fake.removed) > 0) {
				t.Fatal("changed manual label")
			}
		})
	}
}

func TestTitleLabelProcessing(t *testing.T) {
	config := "label-from-title: true\nauto-labels:\n  docs: [docs/**]\n"
	for _, tc := range []struct {
		name       string
		action     string
		title      string
		existing   []model.Label
		explicit   string
		files      []model.ChangedFile
		fileErr    error
		want       string
		added      []string
		removed    []string
		fileReads  int
		changeHead bool
	}{
		{
			name:    "source-feature",
			action:  "opened",
			title:   "✨ Add JSON codec and helpers",
			fileErr: errors.New("file API unavailable"),
			want:    "feature",
			added:   []string{"feature"},
		},
		{
			name:   "title-before-path",
			action: "opened",
			title:  "🐛 Fix documentation examples",
			files:  []model.ChangedFile{{Name: "docs/index.md"}},
			want:   "bug",
			added:  []string{"bug"},
		},
		{
			name:     "manual-category-survives-push",
			action:   "synchronize",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "bug"}},
			fileErr:  errors.New("file API unavailable"),
			want:     "bug",
		},
		{
			name:     "manual-category-survives-edit",
			action:   "edited",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "bug"}},
			want:     "bug",
		},
		{
			name:     "explicit-label-overrides-existing-category",
			action:   "labeled",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "feature"}, {Name: "bug"}, {Name: "help wanted"}},
			explicit: "bug",
			want:     "bug",
			removed:  []string{"feature"},
		},
		{
			name:     "stale-label-event-does-not-restore-removed-category",
			action:   "labeled",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "feature"}},
			explicit: "bug",
			want:     "feature",
		},
		{
			name:     "release-override",
			action:   "synchronize",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "release"}},
			want:     "release",
		},
		{
			name:     "unrelated-label-is-preserved",
			action:   "opened",
			title:    "✨ Add feature",
			existing: []model.Label{{Name: "help wanted"}},
			want:     "feature",
			added:    []string{"feature"},
		},
		{
			name:   "edited-unclassified-title",
			action: "edited",
			title:  "🐛 Fix decoding",
			want:   "bug",
			added:  []string{"bug"},
		},
		{
			name:   "reopened-unclassified-title",
			action: "reopened",
			title:  "✨ Add feature",
			want:   "feature",
			added:  []string{"feature"},
		},
		{
			name:   "release-title",
			action: "opened",
			title:  "🔖 Prepare release",
			want:   "release",
			added:  []string{"release"},
		},
		{
			name:      "unmapped-title-falls-back-to-path",
			action:    "opened",
			title:     "🎉 Initialize guide",
			files:     []model.ChangedFile{{Name: "docs/index.md"}},
			want:      "docs",
			added:     []string{"docs"},
			fileReads: 1,
		},
		{
			name:      "invalid-title-falls-back-to-path",
			action:    "opened",
			title:     "Update guide",
			files:     []model.ChangedFile{{Name: "docs/index.md"}},
			want:      "docs",
			added:     []string{"docs"},
			fileReads: 1,
		},
		{
			name:       "new-head-during-title-classification",
			action:     "opened",
			title:      "✨ Add feature",
			added:      []string{"feature"},
			changeHead: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := fixture()
			event.Action = tc.action
			event.PullRequest.Title = tc.title
			event.PullRequest.Labels = tc.existing
			if tc.explicit != "" {
				event.Label = &model.Label{Name: tc.explicit}
			}
			fake := &fakeGitHub{
				pr:         event.PullRequest,
				config:     config,
				files:      tc.files,
				fileErr:    tc.fileErr,
				changeHead: tc.changeHead,
			}
			if err := NewService(fake, fake).ProcessEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			state, description := "success", "Latest Changes label: "+tc.want
			if tc.changeHead {
				state, description = "", ""
			}
			if fake.status != state || fake.description != description {
				t.Fatalf("got %q %q want %q %q", fake.status, fake.description, state, description)
			}
			if !slices.Equal(fake.added, tc.added) || !slices.Equal(fake.removed, tc.removed) {
				t.Fatalf("added %v removed %v; want added %v removed %v", fake.added, fake.removed, tc.added, tc.removed)
			}
			if fake.fileReads != tc.fileReads {
				t.Fatalf("got %d file requests want %d", fake.fileReads, tc.fileReads)
			}
		})
	}
}

func TestMergedUpdatesRetryWithoutBodyAndPreserveConcurrentChanges(t *testing.T) {
	event := fixture()
	event.Action = "closed"
	event.PullRequest.Labels = []model.Label{{Name: "feature"}}
	fake := &fakeGitHub{
		pr:       event.PullRequest,
		content:  notes.DefaultDocument,
		sha:      "initial-sha",
		conflict: true,
		config:   "auto-labels: {}\ncommit-title: '📝 Refresh release notes'\n",
	}
	service := NewService(fake, fake)
	if err := service.ProcessEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if fake.writes != 2 || fake.message != "📝 Refresh release notes" || strings.ContainsAny(fake.message, "\r\n") {
		t.Fatal(fake.writes, fake.message)
	}
	if !strings.Contains(fake.content, "Concurrent change") || !strings.Contains(fake.content, "PR [#1]") {
		t.Fatal(fake.content)
	}
	if err := service.ProcessEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if fake.writes != 2 {
		t.Fatal("redelivery duplicated note")
	}
}

func TestSkipReleaseAndUnmergedAndOtherBranch(t *testing.T) {
	for _, mode := range []string{"release", "unmerged", "branch", "unknown-action"} {
		t.Run(mode, func(t *testing.T) {
			event := fixture()
			event.Action = "closed"
			event.PullRequest.Labels = []model.Label{{Name: "release"}}
			switch mode {
			case "unmerged":
				event.PullRequest.Merged = false
			case "branch":
				event.PullRequest.Base.Name = "develop"
			case "unknown-action":
				event.Action = "assigned"
			}
			fake := &fakeGitHub{pr: event.PullRequest}
			if err := (NewService(fake, fake)).ProcessEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			if fake.writes != 0 {
				t.Fatal("unexpected write")
			}
			if mode != "release" && fake.reads != 0 {
				t.Fatal("unnecessary API read")
			}
		})
	}
}

func TestCreateMissingNotesAndRejectBodyConfiguration(t *testing.T) {
	event := fixture()
	event.Action = "closed"
	event.PullRequest.Labels = []model.Label{{Name: "bug"}}
	fake := &fakeGitHub{pr: event.PullRequest}
	if err := (NewService(fake, fake)).ProcessEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if fake.writes != 1 || fake.message != notes.DefaultCommitTitle {
		t.Fatal(fake)
	}
	fake = &fakeGitHub{
		pr:     event.PullRequest,
		config: "auto-labels: {}\ncommit-title: |\n  📝 Update notes\n\n  Body\n",
	}
	if err := (NewService(fake, fake)).ProcessEvent(context.Background(), event); err == nil {
		t.Fatal("accepted commit body")
	}
	if fake.writes != 0 {
		t.Fatal("wrote invalid commit")
	}
}
