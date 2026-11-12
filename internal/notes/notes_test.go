package notes

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/hainuoli/latest-changes/internal/model"
)

func TestUpstreamGoldenCases(t *testing.T) {
	data, err := os.ReadFile("testdata/upstream.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string            `json:"name"`
		Input  string            `json:"input"`
		Output string            `json:"output"`
		Error  bool              `json:"error"`
		PR     model.PullRequest `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := GenerateNotes(tc.Input, tc.PR)
			if (err != nil) != tc.Error {
				t.Fatalf("error=%v, expected error=%v", err, tc.Error)
			}
			if !tc.Error && got != tc.Output {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.Output)
			}
		})
	}
}

func TestClassifyIgnoresUnrelatedLabels(t *testing.T) {
	for _, tc := range []struct {
		labels []model.Label
		state  string
	}{
		{nil, "pending"},
		{[]model.Label{{Name: "help wanted"}, {Name: "feature"}}, "success"},
		{[]model.Label{{Name: "feature"}, {Name: "feature"}}, "success"},
		{[]model.Label{{Name: "release"}, {Name: "feature"}}, "failure"},
	} {
		state, _ := ClassifyLabels(tc.labels)
		if state != tc.state {
			t.Fatalf("%v: got %s", tc.labels, state)
		}
	}
}
