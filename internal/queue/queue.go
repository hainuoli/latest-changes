// Package queue provides a single-process, persistent webhook spool.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

const (
	maxAttempts        = 5
	maxJobSize         = 2_000_000
	jobTimeout         = 90 * time.Second
	pollInterval       = time.Second
	completedRetention = 7 * 24 * time.Hour
)

var (
	// ErrFull indicates that no more pending deliveries can be accepted.
	ErrFull = errors.New("webhook queue is full")
	validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type (
	// Queue persists webhook deliveries for a single background worker.
	Queue struct {
		dir      string
		capacity int
		lock     sync.Mutex
	}

	job struct {
		ID       string      `json:"id"`
		Event    model.Event `json:"event"`
		Created  time.Time   `json:"created"`
		Next     time.Time   `json:"next"`
		Attempts int         `json:"attempts"`
		Done     bool        `json:"done"`
		Failed   bool        `json:"failed"`
	}
)

// NewQueue opens a persistent spool for one server process. The directory must
// reside on a persistent local volume; do not share it between replicas.
func NewQueue(dir string, capacity int) (*Queue, error) {
	if capacity < 1 {
		return nil, errors.New("queue capacity must be positive")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("queue path must be a directory, not a symlink")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	q := &Queue{
		dir:      dir,
		capacity: capacity,
	}
	// Fail startup for corrupt persisted tasks instead of silently losing them.
	if _, err := q.readJobs(); err != nil {
		return nil, err
	}
	return q, nil
}

// Enqueue durably saves a delivery or acknowledges an existing delivery ID.
func (q *Queue) Enqueue(id string, event model.Event) error {
	if !validID.MatchString(id) {
		return errors.New("invalid GitHub delivery ID")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	q.lock.Lock()
	defer q.lock.Unlock()
	jobs, err := q.readJobs()
	if err != nil {
		return err
	}
	active := 0
	var failed *job
	for _, item := range jobs {
		if item.ID == id {
			if item.Failed {
				failed = &item
			} else {
				return nil
			}
		}
		if !item.Done && !item.Failed {
			active++
		}
	}
	if active >= q.capacity {
		return ErrFull
	}
	if failed != nil {
		// A manual GitHub redelivery retains its delivery ID.
		failed.Failed, failed.Attempts, failed.Next = false, 0, time.Now().UTC()
		return q.save(*failed)
	}
	now := time.Now().UTC()
	return q.save(job{
		ID:      id,
		Event:   event,
		Created: now,
		Next:    now,
	})
}

// Run processes pending jobs until the context is canceled or storage fails.
func (q *Queue) Run(ctx context.Context, process func(context.Context, model.Event) error) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, err := q.processOne(ctx, process, time.Now().UTC())
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (q *Queue) next(now time.Time) (*job, error) {
	q.lock.Lock()
	defer q.lock.Unlock()
	jobs, err := q.readJobs()
	if err != nil {
		return nil, err
	}
	for _, item := range jobs {
		if item.Done && now.Sub(item.Created) > completedRetention {
			if err := os.Remove(filepath.Join(q.dir, item.ID+".json")); err != nil {
				return nil, err
			}
			continue
		}
		if !item.Done && !item.Failed && !item.Next.After(now) {
			return &item, nil
		}
	}
	return nil, nil
}

func (q *Queue) processOne(ctx context.Context, process func(context.Context, model.Event) error, now time.Time) (bool, error) {
	item, err := q.next(now)
	if err != nil || item == nil {
		return false, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	processErr := process(jobCtx, item.Event)
	cancel()
	if ctx.Err() != nil {
		// Keep pending tasks for the next start.
		return false, nil
	}
	q.lock.Lock()
	defer q.lock.Unlock()
	item.Attempts++
	if processErr == nil {
		item.Done = true
	} else {
		item.Failed = item.Attempts >= maxAttempts
		item.Next = now.Add(time.Duration(1<<item.Attempts) * time.Second)
		slog.Error("Webhook processing failed", "delivery", item.ID, "attempt", item.Attempts, "exhausted", item.Failed, "error", processErr)
	}
	return true, q.save(*item)
}

func (q *Queue) readJobs() ([]job, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil, err
	}
	var jobs []job
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, errors.New("invalid queue entry")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.Size() > maxJobSize {
			return nil, errors.New("oversized queue entry")
		}
		data, err := os.ReadFile(filepath.Join(q.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var item job
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, errors.New("invalid persisted webhook job")
		}
		if !validID.MatchString(item.ID) || entry.Name() != item.ID+".json" || item.Created.IsZero() || item.Attempts < 0 {
			return nil, errors.New("invalid persisted webhook metadata")
		}
		if err := item.Event.Validate(); err != nil {
			return nil, errors.New("invalid persisted webhook payload")
		}
		jobs = append(jobs, item)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Created.Before(jobs[j].Created)
	})
	return jobs, nil
}

func (q *Queue) save(item job) error {
	file, err := os.CreateTemp(q.dir, ".job-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := json.NewEncoder(file).Encode(item); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(q.dir, item.ID+".json")); err != nil {
		return err
	}
	dir, err := os.Open(q.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
