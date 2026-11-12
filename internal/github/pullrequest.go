package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hainuoli/latest-changes/internal/model"
)

const StatusContext = "latest-changes/label"

// GetPullRequest reads and validates the current pull-request state.
func (c *Client) GetPullRequest(ctx context.Context, repo model.Repository, number int, token string) (model.PullRequest, error) {
	var pr model.PullRequest
	err := c.request(ctx, "GET", repoPath(repo)+"/pulls/"+strconv.Itoa(number), token, nil, &pr)
	if err != nil {
		return pr, err
	}
	if pr.Number != number {
		return pr, errors.New("unexpected pull request number")
	}
	return pr, pr.Validate()
}

// GetPullRequestFiles reads all pages and rejects an incomplete file list.
func (c *Client) GetPullRequestFiles(ctx context.Context, repo model.Repository,
	pr model.PullRequest, token string) ([]model.ChangedFile, error) {
	var all []model.ChangedFile
	for page := 1; page <= 100; page++ {
		var files []model.ChangedFile
		endpoint := fmt.Sprintf("%s/pulls/%d/files?per_page=100&page=%d", repoPath(repo), pr.Number, page)
		if err := c.request(ctx, "GET", endpoint, token, nil, &files); err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.Name == "" {
				return nil, errors.New("invalid changed file")
			}
		}
		all = append(all, files...)
		if len(files) < 100 {
			if pr.ChangedFiles != nil && len(all) != *pr.ChangedFiles {
				return nil, errors.New("incomplete pull request file list")
			}
			return all, nil
		}
	}
	return nil, errors.New("pull request file pagination limit exceeded")
}

// AddLabel adds a category without replacing unrelated labels.
func (c *Client) AddLabel(ctx context.Context, repo model.Repository, number int, label, token string) error {
	return c.request(ctx, "POST", fmt.Sprintf("%s/issues/%d/labels", repoPath(repo), number), token, map[string]any{"labels": []string{label}}, nil)
}

// RemoveLabel removes a label, tolerating an already-removed label.
func (c *Client) RemoveLabel(ctx context.Context, repo model.Repository, number int, label, token string) error {
	err := c.request(ctx, "DELETE", fmt.Sprintf("%s/issues/%d/labels/%s", repoPath(repo), number, url.PathEscape(label)), token, nil, nil)
	if HasStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

// SetStatus reports the Latest Changes label check for a commit.
func (c *Client) SetStatus(ctx context.Context, repo model.Repository, sha, state, description, token string) error {
	// GitHub limits status descriptions to 140 characters.
	runes := []rune(description)
	if len(runes) > 140 {
		description = string(runes[:140])
	}
	return c.request(ctx, "POST", repoPath(repo)+"/statuses/"+url.PathEscape(sha), token, map[string]string{
		"state":       state,
		"context":     StatusContext,
		"description": description,
	}, nil)
}
