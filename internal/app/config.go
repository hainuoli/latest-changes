package app

import (
	"context"

	"github.com/hainuoli/latest-changes/internal/labels"
	"github.com/hainuoli/latest-changes/internal/model"
)

const (
	configPath    = ".github/latest-changes.yml"
	maxConfigSize = 100_000
)

func (s *Service) loadConfig(ctx context.Context, repo model.Repository, token string) (*labels.Config, error) {
	file, err := s.client.GetFile(ctx, repo, configPath, repo.DefaultBranch, token)
	if err != nil || file == nil {
		return nil, err
	}

	data, err := file.Decode(maxConfigSize)
	if err != nil {
		return nil, err
	}
	c, err := labels.ParseConfig(data)
	if err != nil {
		return nil, err
	}

	return &c, nil
}
