package model

import "testing"

func TestRepositoryAndPullRequestValidation(t *testing.T) {
	for _, name := range []string{"owner/repo", "hainuoli/latest-changes"} {
		if err := (Repository{
			ID:            1,
			FullName:      name,
			DefaultBranch: "main",
		}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"../repo", "owner/..", "/owner/repo", "owner/repo/extra", "owner/repo?token=secret"} {
		if err := (Repository{
			ID:            1,
			FullName:      name,
			DefaultBranch: "main",
		}).Validate(); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	valid := PullRequest{
		Number: 1,
		Title:  "✨ Add feature",
		URL:    "https://github.com/owner/repo/pull/1",
		User: User{
			Login: "owner",
			URL:   "https://github.com/owner",
		},
		Base: Ref{
			Name: "main",
			SHA:  "base",
		},
		Head: Ref{SHA: "head"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PullRequest){
		func(p *PullRequest) {
			p.URL = "https://evil.example/pull/1"
		},
		func(p *PullRequest) {
			p.Number = 0
		},
		func(p *PullRequest) {
			p.Head.SHA = ""
		},
		func(p *PullRequest) {
			p.Title = "✨ Add feature\nBody"
		},
		func(p *PullRequest) {
			p.User = User{}
		},
		func(p *PullRequest) {
			n := -1
			p.ChangedFiles = &n
		},
	} {
		pr := valid
		mutate(&pr)
		if err := pr.Validate(); err == nil {
			t.Fatal("accepted invalid PR")
		}
	}
}
