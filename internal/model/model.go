package model

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type (
	// User identifies a GitHub account.
	User struct {
		Login string `json:"login"`
		URL   string `json:"html_url"`
	}

	// Label contains a pull-request label name.

	Label struct {
		Name string `json:"name"`
	}

	// Ref identifies a pull-request branch and commit.

	Ref struct {
		Name string `json:"ref"`
		SHA  string `json:"sha"`
	}

	// PullRequest contains the state used for classification and release notes.

	PullRequest struct {
		Number       int     `json:"number"`
		Title        string  `json:"title"`
		URL          string  `json:"html_url"`
		Merged       bool    `json:"merged"`
		User         User    `json:"user"`
		Labels       []Label `json:"labels"`
		ChangedFiles *int    `json:"changed_files"`
		Base         Ref     `json:"base"`
		Head         Ref     `json:"head"`
	}

	// Repository identifies an installation-scoped GitHub repository.

	Repository struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}

	// Event contains a pull-request webhook delivery.

	Event struct {
		Action      string      `json:"action"`
		Repository  Repository  `json:"repository"`
		PullRequest PullRequest `json:"pull_request"`
		Label       *Label      `json:"label"`
	}

	// ChangedFile describes a changed path and its previous name after a rename.

	ChangedFile struct {
		Name         string `json:"filename"`
		PreviousName string `json:"previous_filename"`
		Status       string `json:"status"`
	}
)

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (r Repository) Validate() error {
	if r.ID <= 0 || !repositoryName.MatchString(r.FullName) || r.DefaultBranch == "" {
		return errors.New("invalid repository")
	}
	for _, part := range strings.Split(r.FullName, "/") {
		if part == "." || part == ".." {
			return errors.New("invalid repository")
		}
	}
	return nil
}

func (p PullRequest) Validate() error {
	if p.Number <= 0 || strings.TrimSpace(p.Title) == "" || p.User.Login == "" ||
		p.Base.Name == "" || p.Base.SHA == "" || p.Head.SHA == "" ||
		strings.ContainsAny(p.Title, "\r\n") || strings.ContainsAny(p.User.Login, "\r\n") {
		return errors.New("invalid pull request")
	}
	if p.ChangedFiles != nil && *p.ChangedFiles < 0 {
		return errors.New("invalid changed file count")
	}
	for _, raw := range []string{p.URL, p.User.URL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" ||
			u.User != nil || strings.ContainsAny(raw, "\r\n") {
			return errors.New("invalid GitHub URL")
		}
	}
	return nil
}

func (e Event) Validate() error {
	if e.Action == "" {
		return errors.New("missing event action")
	}
	if err := e.Repository.Validate(); err != nil {
		return err
	}
	return e.PullRequest.Validate()
}
