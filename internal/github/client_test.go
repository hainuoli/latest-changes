package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	client, err := NewClient("Iv23test", string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if handler != nil {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client.baseURL = server.URL
	}
	return client, key
}

func TestJWTSignatureAndClaims(t *testing.T) {
	client, key := testClient(t, nil)
	now := time.Unix(1700000000, 0)
	token, err := client.CreateAppJWT(now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("bad JWT")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatal(err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "Iv23test" || claims["iat"] != float64(now.Add(-time.Minute).Unix()) || claims["exp"] != float64(now.Add(9*time.Minute).Unix()) {
		t.Fatal(claims)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient("client", string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8,
	}))); err != nil {
		t.Fatal(err)
	}
}

func TestClientOptions(t *testing.T) {
	_, key := testClient(t, nil)
	privateKey := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))

	for _, baseURL := range []string{
		"ftp://example.com",
		"https://user:secret@example.com",
		"https://example.com?token=secret",
		"https://example.com#fragment",
		"/relative/path",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewClient("client", privateKey, WithBaseURL(baseURL)); err == nil {
				t.Fatal("accepted an invalid API base URL")
			}
		})
	}

	if _, err := NewClient("client", privateKey, nil); err == nil {
		t.Fatal("accepted a nil option")
	}

	httpClient := &http.Client{
		Timeout: 2 * time.Second,
	}
	client, err := NewClient("client", privateKey,
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

func TestTokenScope(t *testing.T) {
	repo := model.Repository{
		ID:            42,
		FullName:      "owner/repo",
		DefaultBranch: "main",
	}
	for _, bad := range []string{"", "permissions", "repositories", "selection", "expiry", "empty-token"} {
		t.Run(bad, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					t.Error("missing bearer")
				}
				switch r.URL.Path {
				case "/repos/owner/repo/installation":
					fmt.Fprint(w, `{"id":7}`)
				case "/app/installations/7/access_tokens":
					var request struct {
						IDs         []int64           `json:"repository_ids"`
						Permissions map[string]string `json:"permissions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(request.IDs, []int64{42}) || !reflect.DeepEqual(request.Permissions, ContentsPermissions) {
						t.Error("incorrect scope requested")
					}
					result := map[string]any{
						"token":      "installation-secret",
						"expires_at": time.Now().Add(time.Hour),
						"permissions": map[string]string{
							"contents": "write",
							"metadata": "read",
						},
						"repository_selection": "selected",
						"repositories":         []map[string]int{{"id": 42}},
					}
					switch bad {
					case "permissions":
						result["permissions"] = map[string]string{
							"contents":       "write",
							"metadata":       "read",
							"administration": "write",
						}
					case "repositories":
						result["repositories"] = []map[string]int{{"id": 99}}
					case "selection":
						result["repository_selection"] = "all"
					case "expiry":
						result["expires_at"] = time.Now().Add(-time.Hour)
					case "empty-token":
						result["token"] = ""
					}
					json.NewEncoder(w).Encode(result)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(404)
				}
			})
			token, err := client.CreateInstallationToken(context.Background(), repo, ContentsPermissions)
			if bad == "" {
				if err != nil || token != "installation-secret" {
					t.Fatalf("%s %v", token, err)
				}
			} else if err == nil {
				t.Fatal("accepted unsafe token")
			}
		})
	}
}

func TestFilePaginationAndCompleteness(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	}, "token")
	if err != nil || len(files) != 101 {
		t.Fatalf("%d %v", len(files), err)
	}
	count = 102
	if _, err := client.GetPullRequestFiles(context.Background(), repo, model.PullRequest{
		Number:       1,
		ChangedFiles: &count,
	}, "token"); err == nil {
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
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "private-key-secret")
	})
	err := client.SetStatus(context.Background(), model.Repository{FullName: "owner/repo"}, "sha", "success", "ok", "token-secret")
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
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, 302)
	})
	err := client.SetStatus(context.Background(), model.Repository{FullName: "owner/repo"}, "sha", "success", "ok", "secret")
	if !HasStatus(err, 302) || leaked {
		t.Fatal("credentials followed redirect")
	}
}
