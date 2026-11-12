package action

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/hainuoli/latest-changes/internal/app"
	"github.com/hainuoli/latest-changes/internal/config"
	"github.com/hainuoli/latest-changes/internal/github"
	"github.com/hainuoli/latest-changes/internal/model"
)

const maxEventSize = 25_000_000

// Runner processes one Actions event using trusted repository metadata.
type Runner struct {
	client *github.Client
}

// NewRunner creates a single-run processor backed by the GitHub API.
func NewRunner(client *github.Client) *Runner {
	return &Runner{
		client: client,
	}
}

// Run loads the event, verifies its repository, and handles the current PR state.
func (r *Runner) Run(ctx context.Context, cfg config.Config) error {
	event, err := readEvent(cfg.EventPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(event.Repository.FullName, cfg.Repository) {
		return errors.New("event does not match GITHUB_REPOSITORY")
	}

	repo, err := r.client.GetRepository(ctx, cfg.Repository)
	if err != nil {
		return err
	}
	if repo.ID != event.Repository.ID {
		return errors.New("event repository ID does not match the API")
	}
	event.Repository = repo
	if cfg.EventName == "workflow_dispatch" {
		pr, err := r.client.GetPullRequest(ctx, repo, cfg.PullRequest)
		if err != nil {
			return err
		}
		event.PullRequest = pr
		event.Action = "synchronize"
		if pr.State == "closed" || pr.Merged {
			event.Action = "closed"
		}
	}

	if err := app.NewService(r.client).ProcessEvent(ctx, event); err != nil {
		return err
	}
	slog.Info("Latest Changes completed", "repository", repo.FullName,
		"pull_request", event.PullRequest.Number, "action", event.Action)
	return nil
}

func readEvent(path string) (model.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.Event{}, errors.New("could not open GITHUB_EVENT_PATH")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxEventSize+1))
	if err != nil || len(data) > maxEventSize {
		return model.Event{}, errors.New("invalid or oversized Actions event")
	}
	var event model.Event
	if err := json.Unmarshal(data, &event); err != nil {
		return event, errors.New("invalid Actions event JSON")
	}
	return event, nil
}
