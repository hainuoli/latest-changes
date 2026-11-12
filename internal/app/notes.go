package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/labels"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
)

const (
	maxNotesSize      = 1_000_000
	maxUpdateAttempts = 3
)

var notePaths = [...]string{
	"release-notes.md",
	"docs/release-notes.md",
	"docs/en/docs/release-notes.md",
	"CHANGELOG.md",
}

func (s *Service) processMergedEvent(ctx context.Context, repo model.Repository, pr model.PullRequest) error {
	if !pr.Merged {
		return nil
	}

	matching := notes.RecognizedLabels(pr.Labels)
	if len(matching) != 1 {
		return errors.New("merged pull request requires exactly one category")
	}
	if matching[0] == "release" {
		return nil
	}

	c, err := s.loadConfig(ctx, repo)
	if err != nil {
		return err
	}
	title := notes.DefaultCommitTitle
	if c != nil {
		title = c.CommitTitle
	}

	return s.updateNotes(ctx, repo, pr, title)
}

func (s *Service) updateNotes(ctx context.Context, repo model.Repository, pr model.PullRequest, title string) error {
	if err := labels.ValidateCommitTitle(title); err != nil {
		return err
	}

	for attempt := 0; attempt < maxUpdateAttempts; attempt++ {
		file, err := s.findNotes(ctx, repo)
		if err != nil {
			return err
		}

		path, sha, content := notePaths[0], "", notes.DefaultDocument
		if file != nil {
			data, err := file.Decode(maxNotesSize)
			if err != nil {
				return err
			}
			path, sha, content = file.Path, file.SHA, string(data)
		}

		updated, err := notes.GenerateNotes(content, pr)
		if err != nil {
			return err
		}
		if file != nil && updated == content {
			return nil
		}
		if len(updated) > maxNotesSize {
			return errors.New("updated release notes exceed size limit")
		}

		err = s.client.UpdateFile(ctx, repo, path, sha, title, updated)
		if err == nil {
			return nil
		}
		if !github.HasStatus(err, http.StatusConflict) &&
			!(file == nil && github.HasStatus(err, http.StatusUnprocessableEntity)) {
			return err
		}
	}

	return errors.New("release-note update conflicts exceeded retry limit")
}

func (s *Service) findNotes(ctx context.Context, repo model.Repository) (*github.File, error) {
	for _, path := range notePaths {
		file, err := s.client.GetFile(ctx, repo, path, repo.DefaultBranch)
		if err != nil {
			return nil, err
		}
		if file != nil {
			return file, nil
		}
	}

	return nil, nil
}
