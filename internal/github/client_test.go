package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	client, err := NewClient("workflow-token")
	if err != nil {
		t.Fatal(err)
	}
	if handler != nil {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client.baseURL = server.URL
	}
	return client
}

func TestTokenValidation(t *testing.T) {
	for _, token := range []string{"", " ", "token\nsecret", "token secret"} {
		if _, err := NewClient(token); err == nil {
			t.Fatal("accepted an invalid token")
		}
	}
}

func TestClientOptions(t *testing.T) {

	for _, baseURL := range []string{
		"ftp://example.com",
		"https://user:secret@example.com",
		"https://example.com?token=secret",
		"https://example.com#fragment",
		"/relative/path",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewClient("workflow-token", WithBaseURL(baseURL)); err == nil {
				t.Fatal("accepted an invalid API base URL")
			}
		})
	}

	if _, err := NewClient("workflow-token", nil); err == nil {
		t.Fatal("accepted a nil option")
	}

	httpClient := &http.Client{
		Timeout: 2 * time.Second,
	}
	client, err := NewClient("workflow-token",
		WithBaseURL("http://127.0.0.1:8080"), WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != "http://127.0.0.1:8080" || client.httpClient.Timeout != httpClient.Timeout {
		t.Fatal("options were not applied")
	}
	if client.httpClient == httpClient || httpClient.CheckRedirect != nil {
		t.Fatal("the supplied HTTP client was mutated")
	}
}

func TestFilePaginationAndCompleteness(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-token" {
			t.Error("missing workflow token")
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Error("missing page size")
		}
		count := 100
		if r.URL.Query().Get("page") == "2" {
			count = 1
		}
		files := make([]model.ChangedFile, count)
		for i := range files {
			files[i] = model.ChangedFile{Name: fmt.Sprintf("docs/%s-%d.md", r.URL.Query().Get("page"), i)}
		}
		json.NewEncoder(w).Encode(files)
	})
	repo := model.Repository{FullName: "owner/repo"}
	count := 101
	files, err := client.GetPullRequestFiles(context.Background(), repo, model.PullRequest{
		Number:       1,
		ChangedFiles: &count,
	})
	if err != nil || len(files) != 101 {
		t.Fatalf("%d %v", len(files), err)
	}
	count = 102
	if _, err := client.GetPullRequestFiles(context.Background(), repo, model.PullRequest{
		Number:       1,
		ChangedFiles: &count,
	}); err == nil {
		t.Fatal("accepted truncated list")
	}
}

func TestFileDecoding(t *testing.T) {
	valid := File{
		Type:     "file",
		SHA:      "sha",
		Encoding: "base64",
		Content:  "aGVs\nbG8=",
		Size:     5,
	}
	data, err := valid.Decode(5)
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	for _, change := range []func(*File){
		func(f *File) {
			f.Encoding = "utf-8"
		},
		func(f *File) {
			f.Content = "!"
		},
		func(f *File) {
			f.Size = 6
		},
		func(f *File) {
			f.Content = base64.StdEncoding.EncodeToString([]byte{255})
		},
		func(f *File) {
			f.Type = "symlink"
		},
	} {
		file := valid
		change(&file)
		if _, err := file.Decode(5); err == nil {
			t.Fatal("accepted invalid file")
		}
	}
}

func TestErrorsDoNotLeakResponseSecrets(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "response-secret")
	})
	err := client.SetStatus(context.Background(), model.Repository{FullName: "owner/repo"}, "sha", "success", "ok")
	if !HasStatus(err, 401) || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}

func TestNoCredentialRedirect(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = true
	}))
	defer other.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, 302)
	})
	err := client.SetStatus(context.Background(), model.Repository{FullName: "owner/repo"}, "sha", "success", "ok")
	if !HasStatus(err, 302) || leaked {
		t.Fatal("credentials followed redirect")
	}
}
