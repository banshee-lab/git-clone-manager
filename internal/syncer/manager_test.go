package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/events"
	"gitsync/internal/history"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

type fakeExecutor struct {
	mu          sync.Mutex
	active, max int
	gate        chan struct{}
	panic       bool
	output      string
	started     chan struct{}
	failName    string
	runErr      error
}

type countingNotifier struct {
	mu       sync.Mutex
	statuses []model.ActivityStatus
}

func (n *countingNotifier) Notify(_ context.Context, a model.Activity, _ model.Repository) {
	n.mu.Lock()
	n.statuses = append(n.statuses, a.Status)
	n.mu.Unlock()
}
func (n *countingNotifier) snapshot() []model.ActivityStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]model.ActivityStatus(nil), n.statuses...)
}

type failingCleaner struct {
	mu    sync.Mutex
	calls int
}

func (c *failingCleaner) PruneRepository(context.Context, string) (int, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return 0, errors.New("cleanup unavailable")
}

type createFailLogs struct{ logstore.Store }

func (s createFailLogs) Create(string, string) (*logstore.Writer, error) {
	return nil, errors.New("disk full")
}

func makeTestManager(t *testing.T, repos *store.MemoryRepositories, acts *store.MemoryActivities, logs logstore.Store, exec ExecutorRunner, cleaner Cleaner, notifier Notifier, limit int) *Manager {
	t.Helper()
	m, err := NewManager(repos, acts, logs, exec, cleaner, notifier, events.NewHub(), limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return m
}

func makeRepository(t *testing.T, repos *store.MemoryRepositories, name string) model.Repository {
	t.Helper()
	repo := model.Repository{ID: uuid.NewString(), Name: name, URL: "https://example.test/" + name + ".git", Path: t.TempDir() + "/clone"}
	if err := repos.Create(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	return repo
}

func (f *fakeExecutor) Run(ctx context.Context, repo model.Repository, _ Request, out io.Writer) (Result, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.max {
		f.max = f.active
	}
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	if f.panic {
		panic("boom")
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	if f.failName == repo.Name {
		if f.runErr != nil {
			return Result{FailedStage: model.StageMerge, Summary: "merge failed"}, f.runErr
		}
		return Result{FailedStage: model.StageMerge, Summary: "merge failed"}, errors.New("merge failed")
	}
	_, err := io.WriteString(out, f.output)
	if err != nil {
		return Result{}, err
	}
	return Result{Action: model.ActionClone, Summary: "Cloned (abc)", CommitAfter: "abc", ResolvedBranch: "main"}, nil
}

func TestShutdownTimeoutInterruptsRunningSync(t *testing.T) {
	exec := &fakeExecutor{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	m, _, acts, _, repo := setupManager(t, 1, exec)
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-exec.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := acts.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ActivityInterrupted || got.Summary != "Interrupted by shutdown" {
		t.Fatalf("shutdown result = %+v", got)
	}
}

func TestSubmitAllSkipsBusyRepository(t *testing.T) {
	exec := &fakeExecutor{gate: make(chan struct{})}
	m, repos, _, _, repo := setupManager(t, 1, exec)
	if _, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual}); err != nil {
		t.Fatal(err)
	}
	other := model.Repository{ID: uuid.NewString(), Name: "other", URL: "https://example.test/other.git", Path: t.TempDir() + "/other"}
	if err := repos.Create(context.Background(), &other); err != nil {
		t.Fatal(err)
	}
	started, skipped, err := m.SubmitAll(context.Background(), TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if len(started) != 1 || started[0].RepositoryID != other.ID || len(skipped) != 1 || skipped[0].RepositoryID != repo.ID {
		t.Fatalf("SubmitAll started=%+v skipped=%+v", started, skipped)
	}
}

func setupManager(t *testing.T, max int, exec ExecutorRunner) (*Manager, *store.MemoryRepositories, *store.MemoryActivities, *logstore.FS, model.Repository) {
	return setupManagerWith(t, max, exec, nil, nil)
}

func setupManagerWith(t *testing.T, max int, exec ExecutorRunner, cleaner Cleaner, notifier Notifier) (*Manager, *store.MemoryRepositories, *store.MemoryActivities, *logstore.FS, model.Repository) {
	t.Helper()
	repos, acts := store.NewMemory()
	id := uuid.NewString()
	repo := model.Repository{ID: id, Name: "repo", URL: "https://example.test/repo.git", Path: t.TempDir() + "/clone", AccessToken: "canary-secret"}
	if err := repos.Create(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(repos, acts, logs, exec, cleaner, notifier, events.NewHub(), max)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return m, repos, acts, logs, repo
}

func waitActivity(t *testing.T, acts store.Activities, id string) model.Activity {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a, err := acts.Get(context.Background(), id)
		if err == nil && a.Status.IsFinished() {
			return *a
		}
		time.Sleep(5 * time.Millisecond)
	}
	a, _ := acts.Get(context.Background(), id)
	t.Fatalf("activity did not finish: %+v", a)
	return model.Activity{}
}

func TestSubmitPersistsRedactedOutputAndPublishesLifecycle(t *testing.T) {
	exec := &fakeExecutor{output: "result canary-secret\n"}
	m, _, acts, logs, repo := setupManager(t, 1, exec)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventsCh := m.hub.Subscribe(ctx)
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second submit error = %v", err)
	}
	got := waitActivity(t, acts, a.ID)
	if got.Status != model.ActivitySuccess {
		t.Fatalf("status = %s", got.Status)
	}
	data, err := logs.ReadAll(repo.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "canary-secret") || !strings.Contains(string(data), "***") {
		t.Fatalf("secret redaction failed: %s", data)
	}
	var kinds []string
	for len(kinds) < 3 {
		select {
		case e := <-eventsCh:
			kinds = append(kinds, e.Type)
		case <-time.After(time.Second):
			t.Fatalf("missing lifecycle events: %v", kinds)
		}
	}
	if strings.Join(kinds, ",") != "activity.queued,activity.started,activity.finished" {
		t.Fatalf("event order: %v", kinds)
	}
}

func TestLiveLogReadsFileAndClosesSubscriber(t *testing.T) {
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repoID, actID := uuid.NewString(), uuid.NewString()
	writer, err := logs.Create(repoID, actID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := NewLiveLog(repoID, actID, logs, writer)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := live.Subscribe(ctx, 0)
	if _, err := live.Write([]byte("hello 世界")); err != nil {
		t.Fatal(err)
	}
	select {
	case chunk := <-chunks:
		if chunk.Data != "hello 世界" || chunk.Offset != int64(len("hello 世界")) {
			t.Fatalf("chunk = %#v", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("live chunk timed out")
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-chunks:
		if ok {
			t.Fatal("unexpected extra chunk")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not close")
	}
}

func TestLiveLogHandlesRuneSplitAcrossWrites(t *testing.T) {
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repoID, actID := uuid.NewString(), uuid.NewString()
	writer, err := logs.Create(repoID, actID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := NewLiveLog(repoID, actID, logs, writer)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := live.Subscribe(ctx, 0)
	text := "界"
	split := 1
	if _, err := live.Write([]byte(text[:split])); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Write([]byte(text[split:])); err != nil {
		t.Fatal(err)
	}
	select {
	case chunk := <-chunks:
		if chunk.Data != text || chunk.Offset != int64(len(text)) {
			t.Fatalf("chunk = %#v", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("split rune was not emitted")
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverMarksStaleActivitiesAndCreatesLog(t *testing.T) {
	exec := &fakeExecutor{}
	m, _, acts, logs, repo := setupManager(t, 1, exec)
	a := model.Activity{ID: uuid.NewString(), RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivityRunning, Trigger: model.TriggerWebhook, Source: model.SourceOther}
	if err := acts.Create(context.Background(), &a); err != nil {
		t.Fatal(err)
	}
	if err := m.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := acts.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ActivityInterrupted || got.Summary != "Interrupted by restart" {
		t.Fatalf("recovered = %+v", got)
	}
	data, err := logs.ReadAll(repo.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "server restarted") {
		t.Fatalf("restart marker absent: %s", data)
	}
}

func TestPanicMarksFailedAndWorkerContinues(t *testing.T) {
	exec := &fakeExecutor{panic: true}
	m, repos, acts, _, repo := setupManager(t, 1, exec)
	other := makeRepository(t, repos, "other")
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	got := waitActivity(t, acts, a.ID)
	if got.Status != model.ActivityFailed || got.FailedStage != model.StageInternal || !strings.Contains(got.Summary, "panic") {
		t.Fatalf("panic outcome = %+v", got)
	}
	b, err := m.Submit(context.Background(), other.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitActivity(t, acts, b.ID); got.Status != model.ActivityFailed {
		t.Fatalf("worker did not continue after panic: %+v", got)
	}
}

func TestLiveLogSnapshotAndBoundedOutputMemory(t *testing.T) {
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repoID, actID := uuid.NewString(), uuid.NewString()
	writer, err := logs.Create(repoID, actID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := NewLiveLog(repoID, actID, logs, writer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.Write([]byte("snapshot ✓")); err != nil {
		t.Fatal(err)
	}
	snapshot, offset, err := live.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != "snapshot ✓" || offset != int64(len(snapshot)) {
		t.Fatalf("snapshot=%q offset=%d", snapshot, offset)
	}
	chunk := []byte(strings.Repeat("x", 32<<10))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 1600; i++ {
		if _, err := live.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 12<<20 {
		t.Fatalf("50 MiB file output allocated %d bytes; expected bounded streaming", delta)
	}
	if got := live.Size(); got != int64(len("snapshot ✓"))+(50<<20) {
		t.Fatalf("size=%d", got)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrencyLimitQueuesWorkAcrossRepositories(t *testing.T) {
	repos, acts := store.NewMemory()
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exec := &fakeExecutor{gate: make(chan struct{})}
	m := makeTestManager(t, repos, acts, logs, exec, nil, nil, 2)
	items := make([]model.Repository, 5)
	for i := range items {
		items[i] = makeRepository(t, repos, fmt.Sprintf("repo-%d", i))
	}
	ids := make([]string, 0, len(items))
	for _, repo := range items {
		a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec.mu.Lock()
		active := exec.active
		exec.mu.Unlock()
		if active == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	exec.mu.Lock()
	max := exec.max
	active := exec.active
	exec.mu.Unlock()
	if active != 2 || max > 2 {
		t.Fatalf("worker count active=%d max=%d", active, max)
	}
	current, err := acts.ListActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	running, queued := 0, 0
	for _, a := range current {
		if a.Status == model.ActivityRunning {
			running++
		}
		if a.Status == model.ActivityQueued {
			queued++
		}
	}
	if running != 2 || queued != 3 {
		t.Fatalf("while blocked: running=%d queued=%d", running, queued)
	}
	close(exec.gate)
	for _, id := range ids {
		if a := waitActivity(t, acts, id); a.Status != model.ActivitySuccess {
			t.Fatalf("activity %s status %s", id, a.Status)
		}
	}
	exec.mu.Lock()
	max = exec.max
	exec.mu.Unlock()
	if max != 2 {
		t.Fatalf("max concurrent = %d, want 2", max)
	}
}

func TestCleanupPrunesOldestRecordsAndLogsPerRepository(t *testing.T) {
	repos, acts := store.NewMemory()
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := makeRepository(t, repos, "primary")
	other := makeRepository(t, repos, "other")
	cleaner := history.New(acts, repos, logs, 2)
	m := makeTestManager(t, repos, acts, logs, &fakeExecutor{output: "done\n"}, cleaner, nil, 1)
	otherActivity, err := m.Submit(context.Background(), other.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	waitActivity(t, acts, otherActivity.ID)
	var ids []string
	for i := 1; i <= 4; i++ {
		a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
		waitActivity(t, acts, a.ID)
		if i >= 3 {
			removed := ids[i-3]
			if _, err := acts.Get(context.Background(), removed); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("old activity %d record remains: %v", i-2, err)
			}
			if _, err := logs.ReadAll(repo.ID, removed); !errors.Is(err, logstore.ErrNotFound) {
				t.Fatalf("old activity %d log remains: %v", i-2, err)
			}
		}
	}
	if _, err := acts.Get(context.Background(), otherActivity.ID); err != nil {
		t.Fatalf("other repository record was pruned: %v", err)
	}
	if _, err := logs.ReadAll(other.ID, otherActivity.ID); err != nil {
		t.Fatalf("other repository log was pruned: %v", err)
	}
}

func TestCleanupErrorDoesNotFailSync(t *testing.T) {
	cleaner := &failingCleaner{}
	exec := &fakeExecutor{}
	m, _, acts, _, repo := setupManagerWith(t, 1, exec, cleaner, nil)
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitActivity(t, acts, a.ID); got.Status != model.ActivitySuccess {
		t.Fatalf("cleanup error changed sync outcome: %+v", got)
	}
	cleaner.mu.Lock()
	calls := cleaner.calls
	cleaner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("cleanup calls=%d", calls)
	}
}

func TestNotifierCalledOnceForFailuresAndInterruptionsOnly(t *testing.T) {
	notifier := &countingNotifier{}
	exec := &fakeExecutor{failName: "repo"}
	m, repos, acts, _, repo := setupManagerWith(t, 1, exec, nil, notifier)
	stale := model.Activity{ID: uuid.NewString(), RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivityRunning, Trigger: model.TriggerWebhook, Source: model.SourceOther}
	if err := acts.Create(context.Background(), &stale); err != nil {
		t.Fatal(err)
	}
	if err := m.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitActivity(t, acts, failed.ID); got.Status != model.ActivityFailed {
		t.Fatalf("expected failure: %+v", got)
	}
	good := makeRepository(t, repos, "good")
	success, err := m.Submit(context.Background(), good.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitActivity(t, acts, success.ID); got.Status != model.ActivitySuccess {
		t.Fatalf("expected success: %+v", got)
	}
	statuses := notifier.snapshot()
	if len(statuses) != 2 || statuses[0] != model.ActivityInterrupted || statuses[1] != model.ActivityFailed {
		t.Fatalf("notification statuses=%v", statuses)
	}
}

func TestLogCreationFailureMarksActivityInternal(t *testing.T) {
	repos, acts := store.NewMemory()
	repo := makeRepository(t, repos, "repo")
	fs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := makeTestManager(t, repos, acts, createFailLogs{Store: fs}, &fakeExecutor{}, nil, nil, 1)
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	got := waitActivity(t, acts, a.ID)
	if got.Status != model.ActivityFailed || got.FailedStage != model.StageInternal || got.Summary != "cannot write log file: disk full" {
		t.Fatalf("log creation outcome=%+v", got)
	}
}
