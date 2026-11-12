package config

import (
	"errors"
	"os"
	"strconv"

	"github.com/hainuoli/latest-changes/internal/model"
)

// Config contains the GitHub Actions environment for a single run.
type Config struct {
	Token       string
	EventPath   string
	EventName   string
	Repository  string
	PullRequest int
}

// Load reads the runner environment and optional manual recovery input.
func Load() (Config, error) {
	c := Config{
		Token:      os.Getenv("GITHUB_TOKEN"),
		EventPath:  os.Getenv("GITHUB_EVENT_PATH"),
		EventName:  os.Getenv("GITHUB_EVENT_NAME"),
		Repository: os.Getenv("GITHUB_REPOSITORY"),
	}
	if c.Token == "" || c.EventPath == "" || !model.IsRepositoryName(c.Repository) {
		return c, errors.New("GITHUB_TOKEN, GITHUB_EVENT_PATH, and a valid GITHUB_REPOSITORY are required")
	}

	switch c.EventName {
	case "pull_request", "pull_request_target":
		if os.Getenv("LATEST_CHANGES_PULL_REQUEST") != "" {
			return c, errors.New("pull-request input is only supported for workflow_dispatch")
		}
	case "workflow_dispatch":
		number, err := strconv.Atoi(os.Getenv("LATEST_CHANGES_PULL_REQUEST"))
		if err != nil || number <= 0 {
			return c, errors.New("workflow_dispatch requires a positive pull-request input")
		}
		c.PullRequest = number
	default:
		return c, errors.New("expected pull_request, pull_request_target, or workflow_dispatch event")
	}

	return c, nil
}
