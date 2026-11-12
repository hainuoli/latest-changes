package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

const (
	defaultBaseURL   = "https://api.github.com"
	defaultTimeout   = 5 * time.Second
	apiVersion       = "2026-03-10"
	userAgent        = "latest-changes"
	maxResponseBytes = 4_000_000
)

type (
	// Client calls the GitHub API with a repository-scoped workflow token.
	Client struct {
		baseURL    string
		httpClient *http.Client
		token      string
	}

	// ClientOption customizes a Client before it is used.
	ClientOption func(*Client)

	// APIError reports a GitHub HTTP status without exposing response secrets.
	APIError struct {
		StatusCode int
	}
)

// NewClient creates a GitHub client using a token supplied by GitHub Actions.
func NewClient(token string, opts ...ClientOption) (*Client, error) {
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("missing or invalid GitHub token")
	}
	client := &Client{
		baseURL: defaultBaseURL,
		token:   token,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("nil GitHub client option")
		}
		opt(client)
	}
	baseURL, err := url.Parse(client.baseURL)
	if err != nil || baseURL.Host == "" || baseURL.User != nil ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" ||
		(baseURL.Scheme != "https" && baseURL.Scheme != "http") {
		return nil, errors.New("invalid GitHub API base URL")
	}

	if client.httpClient == nil {
		client.httpClient = &http.Client{
			Timeout: defaultTimeout,
		}
	}
	httpClient := *client.httpClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.httpClient = &httpClient

	return client, nil
}

// WithBaseURL selects an API endpoint, for example a local mock server in tests.
func WithBaseURL(baseURL string) ClientOption {
	return func(client *Client) {
		client.baseURL = baseURL
	}
}

// WithHTTPClient sets the HTTP client. Credential redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

// Error returns the sanitized HTTP failure description.
func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API returned HTTP %d", e.StatusCode)
}

// HasStatus reports whether err is a GitHub API error with the given status.
func HasStatus(err error, code int) bool {
	var api *APIError
	return errors.As(err, &api) && api.StatusCode == code
}

func (c *Client) request(ctx context.Context, method, endpoint string, payload, output any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL, "/")+endpoint, body)
	if err != nil {
		return errors.New("invalid GitHub API request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New("GitHub API request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &APIError{response.StatusCode}
	}
	if output == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return errors.New("invalid or oversized GitHub response")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("invalid GitHub JSON response")
	}
	return nil
}

func repoPath(repo model.Repository) string {
	return "/repos/" + repo.FullName
}
