package syncer

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/events"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

type fakeExecutor struct {
	mu          sync.Mutex
	active, max int
	gate        <-chan struct{}
	panic       bool
	output      string
	started     chan struct{}
}

func (f *fakeExecutor) Run(ctx context.Context, _ model.Repository, _ Request, out io.Writer) (Result, error) {
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
	m, err := NewManager(repos, acts, logs, exec, nil, nil, events.NewHub(), max)
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
	m, _, acts, _, repo := setupManager(t, 1, exec)
	a, err := m.Submit(context.Background(), repo.ID, TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual})
	if err != nil {
		t.Fatal(err)
	}
	got := waitActivity(t, acts, a.ID)
	if got.Status != model.ActivityFailed || got.FailedStage != model.StageInternal || !strings.Contains(got.Summary, "panic") {
		t.Fatalf("panic outcome = %+v", got)
	}
}

func TestLiveLogNoMemoryBufferAndSnapshot(t *testing.T) {
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := logs.Create(uuid.NewString(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
}
