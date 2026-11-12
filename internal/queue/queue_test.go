package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

func eventFixture() model.Event {
	return model.Event{
		Action: "opened",
		Repository: model.Repository{
			ID:            1,
			FullName:      "owner/repo",
			DefaultBranch: "main",
		},
		PullRequest: model.PullRequest{
			Number: 1,
			Title:  "✨ Add feature",
			URL:    "https://github.com/owner/repo/pull/1",
			User: model.User{
				Login: "owner",
				URL:   "https://github.com/owner",
			},
			Base: model.Ref{
				Name: "main",
				SHA:  "base",
			},
			Head: model.Ref{SHA: "head"},
		},
	}
}

func TestPersistenceAndConcurrentDeduplication(t *testing.T) {
	dir := t.TempDir()
	q, err := NewQueue(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.Enqueue("delivery-1", eventFixture()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	q, err = NewQueue(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	process := func(context.Context, model.Event) error {
		calls++
		return nil
	}
	worked, err := q.processOne(context.Background(), process, time.Now().Add(time.Second))
	if err != nil || !worked {
		t.Fatal(worked, err)
	}
	if err := q.Enqueue("delivery-1", eventFixture()); err != nil {
		t.Fatal(err)
	}
	worked, err = q.processOne(context.Background(), process, time.Now().Add(2*time.Second))
	if err != nil || worked || calls != 1 {
		t.Fatal(worked, calls, err)
	}
	info, err := os.Stat(filepath.Join(dir, "delivery-1.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("task file permissions", err)
	}
}

func TestBoundedQueueAndPathValidation(t *testing.T) {
	q, err := NewQueue(t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("../escape", eventFixture()); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := q.Enqueue("one", eventFixture()); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("two", eventFixture()); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
}

func TestRetriesSurviveRestartAndFailedDeliveryCanBeRedelivered(t *testing.T) {
	dir := t.TempDir()
	q, err := NewQueue(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("delivery", eventFixture()); err != nil {
		t.Fatal(err)
	}
	fail := func(context.Context, model.Event) error {
		return errors.New("temporary failure")
	}
	for i := 0; i < maxAttempts; i++ {
		q, err = NewQueue(dir, 5)
		if err != nil {
			t.Fatal(err)
		}
		worked, err := q.processOne(context.Background(), fail, time.Now().Add(time.Duration(i+1)*time.Hour))
		if err != nil || !worked {
			t.Fatal(worked, err)
		}
	}
	jobs, err := q.readJobs()
	if err != nil || len(jobs) != 1 || !jobs[0].Failed || jobs[0].Attempts != maxAttempts {
		t.Fatal(jobs, err)
	}
	if err := q.Enqueue("delivery", eventFixture()); err != nil {
		t.Fatal(err)
	}
	worked, err := q.processOne(context.Background(), func(context.Context, model.Event) error {
		return nil
	}, time.Now().Add(time.Second))
	if err != nil || !worked {
		t.Fatal(worked, err)
	}
	jobs, _ = q.readJobs()
	if !jobs[0].Done {
		t.Fatal("manual redelivery not processed")
	}
}

func TestCancellationKeepsTaskPending(t *testing.T) {
	q, err := NewQueue(t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("delivery", eventFixture()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = q.processOne(ctx, func(context.Context, model.Event) error {
		cancel()
		return context.Canceled
	}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := q.readJobs()
	if jobs[0].Done || jobs[0].Attempts != 0 {
		t.Fatal("canceled task was lost")
	}
}

func TestCorruptTaskFailsStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQueue(dir, 1); err == nil {
		t.Fatal("ignored corrupt task")
	}
}
