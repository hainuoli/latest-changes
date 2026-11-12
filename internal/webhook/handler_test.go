package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/queue"
)

type fakeQueue struct {
	calls int
	err   error
}

func (q *fakeQueue) Enqueue(_ string, _ model.Event) error {
	q.calls++
	return q.err
}

func signedRequest(body []byte) *http.Request {
	r := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Delivery", "delivery-1")
	r.Header.Set("X-GitHub-Event", "pull_request")
	return r
}

func payload() []byte {
	e := model.Event{
		Action: "opened",
		Repository: model.Repository{
			ID:            1,
			FullName:      "owner/repo",
			DefaultBranch: "main",
		},
		PullRequest: model.PullRequest{
			Number: 1,
			Title:  "✨ Add feature",
			URL:    "https://github.com/owner/repo/pull/1",
			User: model.User{
				Login: "owner",
				URL:   "https://github.com/owner",
			},
			Base: model.Ref{
				Name: "main",
				SHA:  "base",
			},
			Head: model.Ref{SHA: "head"},
		},
	}
	data, _ := json.Marshal(e)
	return data
}

func TestGitHubSignatureVector(t *testing.T) {
	if !VerifySignature([]byte("Hello, World!"), "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17", "It's a Secret to Everybody") {
		t.Fatal("official test vector failed")
	}
}

func TestWebhookValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     []byte
		mutate   func(*http.Request)
		queueErr error
		status   int
		calls    int
	}{
		{
			name:   "valid",
			body:   payload(),
			status: 202,
			calls:  1,
		},
		{
			name: "forged",
			body: payload(),
			mutate: func(r *http.Request) {
				r.Header.Set("X-Hub-Signature-256", "sha256=00")
			},
			status: 403,
		},
		{
			name: "missing-header",
			body: payload(),
			mutate: func(r *http.Request) {
				r.Header.Del("X-GitHub-Delivery")
			},
			status: 400,
		},
		{
			name:   "malformed",
			body:   []byte("{"),
			status: 400,
		},
		{
			name:   "invalid-payload",
			body:   []byte(`{"action":"opened"}`),
			status: 400,
		},
		{
			name: "ping",
			body: []byte(`{}`),
			mutate: func(r *http.Request) {
				r.Header.Set("X-GitHub-Event", "ping")
			},
			status: 200,
		},
		{
			name:   "oversized",
			body:   []byte(strings.Repeat("x", maxBodyBytes+1)),
			status: 413,
		},
		{
			name:     "persistence-failed",
			body:     payload(),
			queueErr: errors.New("disk full"),
			status:   503,
			calls:    1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeQueue{err: tc.queueErr}
			r := signedRequest(tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			NewHandler("secret", "test", q).ServeHTTP(w, r)
			if w.Code != tc.status || q.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, q.calls, w.Body.String())
			}
		})
	}
}

func TestAcceptedWebhookIsPersistedAndRecoversOnRestart(t *testing.T) {
	dir := t.TempDir()
	q, err := queue.NewQueue(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	NewHandler("secret", "test", q).ServeHTTP(w, signedRequest(payload()))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	q, err = queue.NewQueue(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	NewHandler("secret", "test", q).ServeHTTP(w, signedRequest(payload()))
	if w.Code != 202 {
		t.Fatal("duplicate could not be accepted after restart")
	}
}
