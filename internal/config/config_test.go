package config

import "testing"

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name       string
		event      string
		repository string
		number     string
		want       int
		valid      bool
	}{
		{
			name:       "target",
			event:      "pull_request_target",
			repository: "owner/repo",
			valid:      true,
		},
		{
			name:       "pull-request",
			event:      "pull_request",
			repository: "owner/repo",
			valid:      true,
		},
		{
			name:       "manual",
			event:      "workflow_dispatch",
			repository: "owner/repo",
			number:     "42",
			want:       42,
			valid:      true,
		},
		{
			name:       "missing-input",
			event:      "workflow_dispatch",
			repository: "owner/repo",
		},
		{
			name:       "negative-input",
			event:      "workflow_dispatch",
			repository: "owner/repo",
			number:     "-1",
		},
		{
			name:       "fractional-input",
			event:      "workflow_dispatch",
			repository: "owner/repo",
			number:     "1.5",
		},
		{
			name:       "unexpected-input",
			event:      "pull_request_target",
			repository: "owner/repo",
			number:     "42",
		},
		{
			name:       "unsupported-event",
			event:      "push",
			repository: "owner/repo",
		},
		{
			name:       "invalid-repository",
			event:      "pull_request_target",
			repository: "owner/..",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "workflow-token")
			t.Setenv("GITHUB_EVENT_PATH", "/tmp/event.json")
			t.Setenv("GITHUB_EVENT_NAME", tc.event)
			t.Setenv("GITHUB_REPOSITORY", tc.repository)
			t.Setenv("LATEST_CHANGES_PULL_REQUEST", tc.number)
			cfg, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && (cfg.PullRequest != tc.want || cfg.Token != "workflow-token") {
				t.Fatal("incorrect configuration")
			}
		})
	}
}

func TestMissingRunnerEnvironment(t *testing.T) {
	for _, missing := range []string{"GITHUB_TOKEN", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "workflow-token")
			t.Setenv("GITHUB_EVENT_PATH", "/tmp/event.json")
			t.Setenv("GITHUB_EVENT_NAME", "pull_request_target")
			t.Setenv("GITHUB_REPOSITORY", "owner/repo")
			t.Setenv("LATEST_CHANGES_PULL_REQUEST", "")
			t.Setenv(missing, "")
			if _, err := Load(); err == nil {
				t.Fatal("accepted missing runner environment")
			}
		})
	}
}
