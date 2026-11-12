package app

import (
	"context"

	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/model"
)

type (
	githubClient interface {
		GetPullRequest(ctx context.Context, repo model.Repository, number int) (model.PullRequest, error)
		GetPullRequestFiles(ctx context.Context, repo model.Repository, pr model.PullRequest) ([]model.ChangedFile, error)
		GetFile(ctx context.Context, repo model.Repository, path, ref string) (*github.File, error)
		AddLabel(ctx context.Context, repo model.Repository, number int, label string) error
		RemoveLabel(ctx context.Context, repo model.Repository, number int, label string) error
		SetStatus(ctx context.Context, repo model.Repository, sha, state, description string) error
		UpdateFile(ctx context.Context, repo model.Repository, path, sha, title, content string) error
	}

	// Service handles labels and release notes for the workflow repository.
	Service struct {
		client githubClient
	}
)

// NewService creates a service backed by the given GitHub client.
func NewService(client githubClient) *Service {
	return &Service{
		client: client,
	}
}

// ProcessEvent applies the current pull-request state to labels or release notes.
func (s *Service) ProcessEvent(ctx context.Context, event model.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}

	repo := event.Repository
	if event.PullRequest.Base.Name != repo.DefaultBranch {
		return nil
	}
	if !isLabelAction(event.Action) && !(event.Action == "closed" && event.PullRequest.Merged) {
		return nil
	}

	// Refresh queued runs before acting on their labels or head SHA.
	pr, err := s.client.GetPullRequest(ctx, repo, event.PullRequest.Number)
	if err != nil {
		return err
	}
	if pr.Base.Name != repo.DefaultBranch {
		return nil
	}

	if isLabelAction(event.Action) {
		return s.processLabelEvent(ctx, event, pr)
	}

	return s.processMergedEvent(ctx, repo, pr)
}

func isLabelAction(action string) bool {
	switch action {
	case "opened", "edited", "synchronize", "reopened", "labeled", "unlabeled":
		return true
	default:
		return false
	}
}
