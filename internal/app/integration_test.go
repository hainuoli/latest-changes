package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
	"github.com/hainuoli/latest-changes/internal/queue"
	"github.com/hainuoli/latest-changes/internal/webhook"
)

func TestWebhookToGitHubEndToEnd(t *testing.T) {
	event := fixture()
	event.Action = "closed"
	event.PullRequest.Labels = []model.Label{{Name: "feature"}}
	var written struct {
		Message string

		Content string

		SHA string

		Branch string
	}
	writeCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Error("missing API version")
		}
		switch r.URL.Path {
		case "/repos/owner/repo/installation":
			fmt.Fprint(w, `{"id":7}`)
		case "/app/installations/7/access_tokens":
			var payload struct {
				IDs []int64 `json:"repository_ids"`

				Permissions map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload.IDs) != 1 || payload.IDs[0] != 42 {
				t.Error("token not scoped to target repository")
			}
			payload.Permissions["metadata"] = "read"
			json.NewEncoder(w).Encode(map[string]any{
				"token":                "token",
				"expires_at":           time.Now().Add(time.Hour),
				"permissions":          payload.Permissions,
				"repository_selection": "selected",
				"repositories":         []map[string]int{{"id": 42}},
			})
		case "/repos/owner/repo/pulls/1":
			json.NewEncoder(w).Encode(event.PullRequest)
		case "/repos/owner/repo/contents/.github/latest-changes.yml":
			text := "auto-labels: {}\ncommit-title: '📝 Refresh release notes'\n"
			json.NewEncoder(w).Encode(github.File{
				Type:     "file",
				Path:     configPath,
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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	client, err := github.NewClient("client", string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})), github.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}

	spool, err := queue.NewQueue(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	request := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(string(body)))
	request.Header.Set("X-GitHub-Delivery", "end-to-end-delivery")
	request.Header.Set("X-GitHub-Event", "pull_request")
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := httptest.NewRecorder()
	webhook.NewHandler("secret", "test", spool).ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatalf("not accepted: %d %s", response.Code, response.Body.String())
	}
	// This runs the actual API client and queue worker, never a live GitHub API.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processed := make(chan error, 1)
	workerDone := make(chan error, 1)
	service := NewService(client)
	go func() {
		workerDone <- spool.Run(ctx, func(ctx context.Context, event model.Event) error {
			err := service.ProcessEvent(ctx, event)
			processed <- err
			return err
		})
	}()
	select {
	case err := <-processed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker timed out")
	}
	cancel()
	if err := <-workerDone; err != nil {
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
