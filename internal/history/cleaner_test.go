package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

type failingLogs struct {
	logstore.Store
	fail string
}

func (f failingLogs) Remove(repoID, actID string) error {
	if actID == f.fail {
		return errors.New("injected remove failure")
	}
	return f.Store.Remove(repoID, actID)
}

func setup(t *testing.T, max int, logs logstore.Store) (*Cleaner, store.Repositories, store.Activities, *logstore.FS, string) {
	t.Helper()
	repos, acts := store.NewMemory()
	fs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := model.Repository{ID: uuid.NewString(), Name: "repo"}
	if err := repos.Create(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = fs
	}
	return New(acts, repos, logs, max), repos, acts, fs, r.ID
}
func addActivity(t *testing.T, acts store.Activities, logs *logstore.FS, repoID string, status model.ActivityStatus, created time.Time) model.Activity {
	t.Helper()
	finished := created.Add(time.Minute)
	a := model.Activity{ID: uuid.NewString(), RepositoryID: repoID, Status: status, Trigger: model.TriggerManual, Source: model.SourceManual, CreatedAt: created, FinishedAt: &finished}
	if status == model.ActivityRunning || status == model.ActivityQueued {
		a.FinishedAt = nil
	}
	if err := acts.Create(context.Background(), &a); err != nil {
		t.Fatal(err)
	}
	if err := logs.Append(repoID, a.ID, "activity"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPruneRemovesOldestFinishedAndLogsOnly(t *testing.T) {
	c, _, acts, logs, repoID := setup(t, 3, nil)
	items := make([]model.Activity, 0, 9)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 7; i++ {
		status := model.ActivitySuccess
		if i%3 == 1 {
			status = model.ActivityFailed
		}
		if i%3 == 2 {
			status = model.ActivityInterrupted
		}
		items = append(items, addActivity(t, acts, logs, repoID, status, base.Add(time.Duration(i)*time.Minute)))
	}
	queued := addActivity(t, acts, logs, repoID, model.ActivityQueued, base.Add(time.Hour))
	running := addActivity(t, acts, logs, repoID, model.ActivityRunning, base.Add(2*time.Hour))
	n, err := c.PruneRepository(context.Background(), repoID)
	if err != nil || n != 4 {
		t.Fatalf("PruneRepository = %d, %v", n, err)
	}
	for i, a := range items {
		_, err := acts.Get(context.Background(), a.ID)
		if i < 4 && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("old activity %s retained: %v", a.ID, err)
		}
		if i >= 4 && err != nil {
			t.Fatalf("new activity removed: %v", err)
		}
		exists, e := logs.Exists(repoID, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		if (i >= 4) != exists {
			t.Fatalf("activity %d file exists=%v", i, exists)
		}
	}
	for _, a := range []model.Activity{queued, running} {
		if _, err := acts.Get(context.Background(), a.ID); err != nil {
			t.Fatalf("active activity removed: %v", err)
		}
	}
}

func TestPruneFailureKeepsRecordAndRetries(t *testing.T) {
	_, _, acts, logs, repoID := setup(t, 1, nil)
	base := time.Now().Add(-time.Hour)
	old1 := addActivity(t, acts, logs, repoID, model.ActivitySuccess, base)
	old2 := addActivity(t, acts, logs, repoID, model.ActivityFailed, base.Add(time.Minute))
	newest := addActivity(t, acts, logs, repoID, model.ActivityInterrupted, base.Add(2*time.Minute))
	c := New(acts, nil, failingLogs{Store: logs, fail: old1.ID}, 1)
	n, err := c.PruneRepository(context.Background(), repoID)
	if err != nil || n != 1 {
		t.Fatalf("first prune = %d, %v", n, err)
	}
	if _, err := acts.Get(context.Background(), old1.ID); err != nil {
		t.Fatalf("failed removal record lost: %v", err)
	}
	if _, err := acts.Get(context.Background(), old2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other old record retained: %v", err)
	}
	c = New(acts, nil, logs, 1)
	n, err = c.PruneRepository(context.Background(), repoID)
	if err != nil || n != 1 {
		t.Fatalf("retry prune = %d, %v", n, err)
	}
	if _, err := acts.Get(context.Background(), newest.ID); err != nil {
		t.Fatalf("newest record removed: %v", err)
	}
}

func TestDeleteRepositoryAndStartupSweep(t *testing.T) {
	c, repos, acts, logs, repoID := setup(t, 2, nil)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 4; i++ {
		addActivity(t, acts, logs, repoID, model.ActivitySuccess, base.Add(time.Duration(i)*time.Minute))
	}
	orphanID := uuid.NewString()
	if err := logs.Append(repoID, orphanID, "orphan"); err != nil {
		t.Fatal(err)
	}
	orphanPath, _ := logs.Path(repoID, orphanID)
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(orphanPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := c.Startup(context.Background()); err != nil {
		t.Fatal(err)
	}
	activities, err := acts.ListByRepository(context.Background(), repoID, 0, nil)
	if err != nil || len(activities) != 2 {
		t.Fatalf("startup activities=%d err=%v", len(activities), err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan remains: %v", err)
	}
	if err := c.DeleteRepository(context.Background(), repoID); err != nil {
		t.Fatal(err)
	}
	activities, err = acts.ListByRepository(context.Background(), repoID, 0, nil)
	if err != nil || len(activities) != 0 {
		t.Fatalf("after delete activities=%d err=%v", len(activities), err)
	}
	logPath, _ := logs.Path(repoID, uuid.NewString())
	if _, err = os.Stat(filepath.Dir(logPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repo logs remain: %v", err)
	}
	if err := repos.Delete(context.Background(), repoID); err != nil {
		t.Fatal(err)
	}
}

func TestPruneNoopWhenUnderLimit(t *testing.T) {
	c, _, acts, logs, repoID := setup(t, 50, nil)
	a := addActivity(t, acts, logs, repoID, model.ActivitySuccess, time.Now())
	n, err := c.PruneRepository(context.Background(), repoID)
	if err != nil || n != 0 {
		t.Fatalf("PruneRepository=%d,%v", n, err)
	}
	if _, err := acts.Get(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
}
