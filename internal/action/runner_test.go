package action

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hainuoli/latest-changes/internal/config"
	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
)

func TestActionsEventToGitHubEndToEnd(t *testing.T) {
	event := fixture()
	event.Action = "closed"
	event.PullRequest.Labels = []model.Label{{Name: "feature"}}
	var written struct {
		Message string
		Content string
		SHA     string
		Branch  string
	}
	writeCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-token" {
			t.Error("missing workflow token")
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Error("missing API version")
		}
		switch r.URL.Path {
		case "/repos/owner/repo":
			json.NewEncoder(w).Encode(event.Repository)
		case "/repos/owner/repo/pulls/1":
			json.NewEncoder(w).Encode(event.PullRequest)
		case "/repos/owner/repo/contents/.github/latest-changes.yml":
			text := "auto-labels: {}\ncommit-title: '📝 Refresh release notes'\n"
			json.NewEncoder(w).Encode(github.File{
				Type:     "file",
				Path:     ".github/latest-changes.yml",
				SHA:      "config-sha",
				Size:     len(text),
				Encoding: "base64",
				Content:  base64.StdEncoding.EncodeToString([]byte(text)),
			})
		case "/repos/owner/repo/contents/release-notes.md":
			w.WriteHeader(404)
		case "/repos/owner/repo/contents/docs/release-notes.md":
			if r.Method == "GET" {
				json.NewEncoder(w).Encode(github.File{
					Type:     "file",
					Path:     "docs/release-notes.md",
					SHA:      "initial-sha",
					Size:     len(notes.DefaultDocument),
					Encoding: "base64",
					Content:  base64.StdEncoding.EncodeToString([]byte(notes.DefaultDocument)),
				})
			} else if r.Method == "PUT" {
				if err := json.NewDecoder(r.Body).Decode(&written); err != nil {
					t.Error(err)
				}
				writeCount++
				w.WriteHeader(201)
			} else {
				t.Errorf("unexpected method %s", r.Method)
				w.WriteHeader(405)
			}
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	client, err := github.NewClient("workflow-token", github.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "event.json")
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		EventPath:  path,
		EventName:  "pull_request_target",
		Repository: "owner/repo",
	}
	if err := NewRunner(client).Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	content, err := base64.StdEncoding.DecodeString(written.Content)
	if err != nil {
		t.Fatal(err)
	}
	if writeCount != 1 || written.Message != "📝 Refresh release notes" || strings.ContainsAny(written.Message, "\r\n") || written.SHA != "initial-sha" || written.Branch != "main" || !strings.Contains(string(content), "PR [#1]") {
		t.Fatalf("unexpected write: %+v", written)
	}
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

func TestRejectUntrustedEventRepository(t *testing.T) {
	for _, tc := range []struct {
		name        string
		repoName    string
		repoID      int64
		invalidJSON bool
		apiCalls    int
	}{
		{
			name:     "different-repository",
			repoName: "other/repo",
			repoID:   42,
		},
		{
			name:     "different-id",
			repoName: "owner/repo",
			repoID:   99,
			apiCalls: 1,
		},
		{
			name:        "invalid-json",
			invalidJSON: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := fixture()
			event.Repository.FullName = tc.repoName
			event.Repository.ID = tc.repoID
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/repos/owner/repo" {
					t.Error("untrusted event reached a PR or write endpoint")
				}
				json.NewEncoder(w).Encode(fixture().Repository)
			}))
			defer api.Close()
			client, err := github.NewClient("workflow-token", github.WithBaseURL(api.URL))
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalidJSON {
				body = []byte(`{"repository":`)
			}
			path := filepath.Join(t.TempDir(), "event.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{
				EventPath:  path,
				EventName:  "pull_request_target",
				Repository: "owner/repo",
			}
			if err := NewRunner(client).Run(context.Background(), cfg); err == nil {
				t.Fatal("accepted an untrusted event")
			}
			if calls != tc.apiCalls {
				t.Fatalf("got %d API calls, want %d", calls, tc.apiCalls)
			}
		})
	}
}

func TestManualRecovery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		state      string
		merged     bool
		wantStatus int
		wantWrites int
	}{
		{
			name:       "open",
			state:      "open",
			wantStatus: 1,
		},
		{
			name:       "merged",
			state:      "closed",
			merged:     true,
			wantWrites: 1,
		},
		{
			name:  "closed-without-merge",
			state: "closed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := fixture()
			pr := event.PullRequest
			pr.State = tc.state
			pr.Merged = tc.merged
			pr.Labels = []model.Label{{Name: "bug"}}
			statuses, writes := 0, 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo":
					json.NewEncoder(w).Encode(event.Repository)
				case "/repos/owner/repo/pulls/1":
					json.NewEncoder(w).Encode(pr)
				case "/repos/owner/repo/statuses/new-head":
					statuses++
					w.WriteHeader(201)
				case "/repos/owner/repo/contents/release-notes.md":
					if r.Method == "PUT" {
						writes++
						w.WriteHeader(201)
					} else {
						w.WriteHeader(404)
					}
				default:
					if strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/") {
						w.WriteHeader(404)
					} else {
						t.Errorf("unexpected API request %s %s", r.Method, r.URL)
						w.WriteHeader(404)
					}
				}
			}))
			defer api.Close()
			client, err := github.NewClient("workflow-token", github.WithBaseURL(api.URL))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "event.json")
			body, err := json.Marshal(map[string]any{"repository": event.Repository})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{
				EventPath:   path,
				EventName:   "workflow_dispatch",
				Repository:  "owner/repo",
				PullRequest: 1,
			}
			if err := NewRunner(client).Run(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			if statuses != tc.wantStatus || writes != tc.wantWrites {
				t.Fatalf("statuses=%d writes=%d", statuses, writes)
			}
		})
	}
}

func TestActionsClassifyDocumentation(t *testing.T) {
	event := fixture()
	event.PullRequest.State = "open"
	event.PullRequest.Merged = false
	event.PullRequest.Head.SHA = "stale-event-head"
	current := event.PullRequest
	current.Head.SHA = "current-head"
	statusWrites := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-token" {
			t.Error("missing workflow token")
		}
		switch r.URL.Path {
		case "/repos/owner/repo":
			json.NewEncoder(w).Encode(event.Repository)
		case "/repos/owner/repo/pulls/1":
			json.NewEncoder(w).Encode(current)
		case "/repos/owner/repo/pulls/1/files":
			json.NewEncoder(w).Encode([]model.ChangedFile{{Name: "docs/guide.md"}})
		case "/repos/owner/repo/contents/.github/latest-changes.yml":
			if r.URL.Query().Get("ref") != "main" {
				t.Error("configuration was not read from the default branch")
			}
			content := "auto-labels:\n  docs: [docs/**]\n"
			json.NewEncoder(w).Encode(github.File{
				Type:     "file",
				Path:     ".github/latest-changes.yml",
				SHA:      "config-sha",
				Size:     len(content),
				Encoding: "base64",
				Content:  base64.StdEncoding.EncodeToString([]byte(content)),
			})
		case "/repos/owner/repo/issues/1/labels":
			if r.Method != "POST" {
				t.Error("unexpected label operation")
			}
			var payload struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload.Labels) != 1 || payload.Labels[0] != "docs" {
				t.Error("incorrect automatic label")
			}
			current.Labels = []model.Label{{Name: "docs"}}
			w.WriteHeader(200)
		case "/repos/owner/repo/statuses/current-head":
			var payload struct {
				State   string `json:"state"`
				Context string `json:"context"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.State != "success" || payload.Context != github.StatusContext {
				t.Error("incorrect label status")
			}
			statusWrites++
			w.WriteHeader(201)
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	client, err := github.NewClient("workflow-token", github.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "event.json")
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		EventPath:  path,
		EventName:  "pull_request_target",
		Repository: "owner/repo",
	}
	if err := NewRunner(client).Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if statusWrites != 1 || len(current.Labels) != 1 || current.Labels[0].Name != "docs" {
		t.Fatal("documentation was not classified")
	}
}
