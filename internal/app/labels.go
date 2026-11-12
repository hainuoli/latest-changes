package app

import (
	"context"
	"errors"

	"github.com/hainuoli/latest-changes/internal/labels"
	"github.com/hainuoli/latest-changes/internal/model"
	"github.com/hainuoli/latest-changes/internal/notes"
)

func (s *Service) processLabelEvent(ctx context.Context, event model.Event, pr model.PullRequest) error {
	repo := event.Repository
	c, err := s.loadConfig(ctx, repo)
	if err != nil {
		return s.client.SetStatus(ctx, repo, pr.Head.SHA, "failure",
			"Invalid or unavailable .github/latest-changes.yml configuration")
	}

	state, description := notes.ClassifyLabels(pr.Labels)
	if c != nil {
		selected, err := s.selectLabel(ctx, event, pr, *c)
		if err != nil {
			return s.client.SetStatus(ctx, repo, pr.Head.SHA, "failure", err.Error())
		}
		if err := s.applyLabel(ctx, repo, pr, selected); err != nil {
			return err
		}

		state, description = "pending", "Waiting for one Latest Changes label"
		if selected != "" {
			state, description = "success", "Latest Changes label: "+selected
		}
	}

	// A newer push has its own delivery. Its status must use its own file list.
	latest, err := s.client.GetPullRequest(ctx, repo, pr.Number)
	if err != nil {
		return err
	}
	if latest.Head.SHA != pr.Head.SHA || latest.Base.Name != repo.DefaultBranch {
		return nil
	}

	return s.client.SetStatus(ctx, repo, pr.Head.SHA, state, description)
}

func (s *Service) selectLabel(ctx context.Context, event model.Event, pr model.PullRequest,
	c labels.Config) (string, error) {
	var explicit string
	if event.Action == "labeled" && event.Label != nil {
		explicit = event.Label.Name
	}

	if c.LabelFromTitle {
		// Existing categories take precedence so automatic classification cannot
		// undo a maintainer's choice on later pushes or title edits.
		if selected := labels.SelectLabel(pr.Labels, explicit, ""); selected != "" {
			return selected, nil
		}
		if candidate := labels.LabelFromTitle(pr.Title); candidate != "" {
			return candidate, nil
		}
	}

	var candidate string
	if event.Action == "opened" || event.Action == "synchronize" {
		files, err := s.client.GetPullRequestFiles(ctx, event.Repository, pr)
		if err != nil {
			if len(notes.RecognizedLabels(pr.Labels)) == 0 {
				return "", errors.New("could not determine an automatic label")
			}
		} else {
			candidate = c.SelectCandidate(files)
		}
	}

	return labels.SelectLabel(pr.Labels, explicit, candidate), nil
}

func (s *Service) applyLabel(ctx context.Context, repo model.Repository, pr model.PullRequest,
	selected string) error {
	if selected != "" && !hasLabel(pr.Labels, selected) {
		if err := s.client.AddLabel(ctx, repo, pr.Number, selected); err != nil {
			return err
		}
	}

	for _, label := range notes.RecognizedLabels(pr.Labels) {
		if label == selected {
			continue
		}
		if err := s.client.RemoveLabel(ctx, repo, pr.Number, label); err != nil {
			return err
		}
	}

	return nil
}

func hasLabel(existing []model.Label, name string) bool {
	for _, label := range existing {
		if label.Name == name {
			return true
		}
	}

	return false
}
