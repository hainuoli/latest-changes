package github

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/hainuoli/latest-changes/internal/model"
)

type (
	// File contains a GitHub Contents API file response.
	File struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		SHA      string `json:"sha"`
		Size     int    `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}

	commitIdentity struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
)

// GetRepository resolves the workflow repository and its current default branch.
func (c *Client) GetRepository(ctx context.Context, fullName string) (model.Repository, error) {
	if !model.IsRepositoryName(fullName) {
		return model.Repository{}, errors.New("invalid repository name")
	}
	var repo model.Repository
	if err := c.request(ctx, "GET", "/repos/"+fullName, nil, &repo); err != nil {
		return repo, err
	}
	if !strings.EqualFold(repo.FullName, fullName) {
		return repo, errors.New("unexpected repository response")
	}
	return repo, repo.Validate()
}

// GetFile reads a file at the given ref, returning nil when it does not exist.
func (c *Client) GetFile(ctx context.Context, repo model.Repository, path, ref string) (*File, error) {
	var file File
	err := c.request(ctx, "GET", repoPath(repo)+"/contents/"+escapePath(path)+"?ref="+url.QueryEscape(ref), nil, &file)
	if HasStatus(err, http.StatusNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if file.Path != path {
		return nil, errors.New("unexpected repository file path")
	}
	return &file, nil
}

// UpdateFile writes release notes using the expected blob SHA.
// Commit metadata stays attributed to the bot regardless of the token owner;
// GitHub still authorizes and audits the write using the authenticated identity.
func (c *Client) UpdateFile(ctx context.Context, repo model.Repository, path, sha, title, content string) error {
	identity := commitIdentity{
		Name:  "github-actions[bot]",
		Email: "41898282+github-actions[bot]@users.noreply.github.com",
	}
	payload := struct {
		Message   string         `json:"message"`
		Content   string         `json:"content"`
		Branch    string         `json:"branch"`
		SHA       string         `json:"sha,omitempty"`
		Author    commitIdentity `json:"author"`
		Committer commitIdentity `json:"committer"`
	}{
		Message:   title,
		Content:   base64.StdEncoding.EncodeToString([]byte(content)),
		Branch:    repo.DefaultBranch,
		SHA:       sha,
		Author:    identity,
		Committer: identity,
	}
	return c.request(ctx, "PUT", repoPath(repo)+"/contents/"+escapePath(path), payload, nil)
}

// Decode validates and decodes the bounded UTF-8 file contents.
func (f File) Decode(limit int) ([]byte, error) {
	if f.Type != "file" || f.SHA == "" || f.Size < 0 || f.Size > limit || f.Encoding != "base64" {
		return nil, errors.New("invalid repository file metadata")
	}
	encoded := strings.Join(strings.Fields(f.Content), "")
	if len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New("repository file too large")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > limit || !utf8.Valid(data) {
		return nil, errors.New("repository file is not valid UTF-8 base64")
	}
	return data, nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
