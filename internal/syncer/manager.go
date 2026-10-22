package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/events"
	"gitsync/internal/gitx"
	"gitsync/internal/history"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

var ErrBusy = errors.New("repository already has a queued or running sync")
var ErrStopping = errors.New("sync manager is stopping")

type TriggerInfo struct {
	Trigger     model.Trigger
	Source      model.Source
	TriggeredBy string
}
type ExecutorRunner interface {
	Run(context.Context, model.Repository, Request, io.Writer) (Result, error)
}
type Notifier interface {
	Notify(context.Context, model.Activity, model.Repository)
}
type Cleaner interface {
	PruneRepository(context.Context, string) (int, error)
}

type Manager struct {
	repos      store.Repositories
	activities store.Activities
	logs       logstore.Store
	executor   ExecutorRunner
	cleaner    Cleaner
	notifier   Notifier
	hub        *events.Hub
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	jobs       chan model.Activity
	workers    sync.WaitGroup
	tasks      sync.WaitGroup
	mu         sync.Mutex
	accepting  bool
	busy       map[string]bool
	live       map[string]*LiveLog
}

func NewManager(repos store.Repositories, activities store.Activities, logs logstore.Store, executor ExecutorRunner, cleaner Cleaner, notifier Notifier, hub *events.Hub, maxConcurrent int) (*Manager, error) {
	if repos == nil || activities == nil || logs == nil || executor == nil {
		return nil, errors.New("syncer: manager dependencies are incomplete")
	}
	if maxConcurrent < 1 {
		return nil, errors.New("syncer: max concurrent must be at least 1")
	}
	if hub == nil {
		hub = events.NewHub()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{repos: repos, activities: activities, logs: logs, executor: executor, cleaner: cleaner, notifier: notifier, hub: hub, logger: slog.Default().With("component", "syncer"), ctx: ctx, cancel: cancel, jobs: make(chan model.Activity, 65536), accepting: true, busy: make(map[string]bool), live: make(map[string]*LiveLog)}
	for range maxConcurrent {
		m.workers.Add(1)
		go m.worker()
	}
	return m, nil
}

func (m *Manager) Submit(ctx context.Context, repoID string, info TriggerInfo) (model.Activity, error) {
	if ctx == nil {
		return model.Activity{}, errors.New("syncer: context is nil")
	}
	if !info.Trigger.Valid() || !info.Source.Valid() {
		return model.Activity{}, errors.New("syncer: invalid trigger information")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.accepting {
		return model.Activity{}, ErrStopping
	}
	if m.busy[repoID] {
		return model.Activity{}, ErrBusy
	}
	repo, err := m.repos.Get(ctx, repoID)
	if err != nil {
		return model.Activity{}, fmt.Errorf("syncer: get repository: %w", err)
	}
	a := model.Activity{ID: uuid.NewString(), RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivityQueued, Trigger: info.Trigger, Source: info.Source, TriggeredBy: info.TriggeredBy, CreatedAt: time.Now().UTC()}
	if err := m.activities.Create(ctx, &a); err != nil {
		return model.Activity{}, fmt.Errorf("syncer: create queued activity: %w", err)
	}
	m.busy[repoID] = true
	m.tasks.Add(1)
	m.publish("activity.queued", a)
	m.jobs <- a
	return a, nil
}

type Skip struct {
	RepositoryID string `json:"repository_id"`
	Reason       string `json:"reason"`
}

func (m *Manager) SubmitAll(ctx context.Context, info TriggerInfo) ([]model.Activity, []Skip, error) {
	repos, err := m.repos.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("syncer: list repositories: %w", err)
	}
	started := make([]model.Activity, 0, len(repos))
	skipped := make([]Skip, 0)
	for _, repo := range repos {
		a, err := m.Submit(ctx, repo.ID, info)
		if errors.Is(err, ErrBusy) {
			skipped = append(skipped, Skip{RepositoryID: repo.ID, Reason: "already queued or running"})
			continue
		}
		if err != nil {
			return started, skipped, err
		}
		started = append(started, a)
	}
	return started, skipped, nil
}

func (m *Manager) IsBusy(repoID string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.busy[repoID] }
func (m *Manager) LiveLog(activityID string) *LiveLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live[activityID]
}
func (m *Manager) publish(kind string, a model.Activity) {
	m.hub.Publish(events.Event{Type: kind, Data: map[string]any{"activity": a}})
}

func (m *Manager) worker() {
	defer m.workers.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case a := <-m.jobs:
			m.runActivity(a)
		}
	}
}

func (m *Manager) runActivity(a model.Activity) {
	defer m.tasks.Done()
	if m.ctx.Err() != nil {
		m.interruptQueued(a)
		return
	}
	var live *LiveLog
	defer func() {
		if p := recover(); p != nil {
			m.finishFailure(context.Background(), a, model.StageInternal, fmt.Sprintf("executor panic: %v", p), live)
		}
	}()
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()
	now := time.Now().UTC()
	a.Status = model.ActivityRunning
	a.StartedAt = &now
	if err := m.activities.Update(ctx, &a); err != nil {
		m.failInternal(a, "cannot mark activity running: "+err.Error())
		return
	}
	m.publish("activity.started", a)
	writer, err := m.logs.Create(a.RepositoryID, a.ID)
	if err != nil {
		m.finishFailure(ctx, a, model.StageInternal, "cannot write log file: "+err.Error(), nil)
		return
	}
	live, err = NewLiveLog(a.RepositoryID, a.ID, m.logs, writer)
	if err != nil {
		_ = writer.Close()
		m.finishFailure(ctx, a, model.StageInternal, "cannot initialize live log: "+err.Error(), nil)
		return
	}
	m.mu.Lock()
	m.live[a.ID] = live
	m.mu.Unlock()
	repo, err := m.repos.Get(ctx, a.RepositoryID)
	if err != nil {
		_, _ = fmt.Fprintf(live, "[gitsync] internal error: %v\n", err)
		m.finishFailure(ctx, a, model.StageInternal, "cannot load repository: "+err.Error(), live)
		return
	}
	username := repo.AccessUsername
	if username == "" {
		username = "git"
	}
	redactor := gitx.NewRedactor(live, repo.AccessToken, username+":"+repo.AccessToken)
	result, runErr := m.executor.Run(ctx, *repo, Request{Trigger: a.Trigger, Source: a.Source, TriggeredBy: a.TriggeredBy}, redactor)
	if closeErr := redactor.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
		result.FailedStage = model.StageInternal
		result.Summary = "cannot flush redacted output: " + closeErr.Error()
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			m.finishInterrupted(a, "Interrupted by shutdown", live, repo)
			return
		}
		a.Action = result.Action
		a.CommitBefore = result.CommitBefore
		a.CommitAfter = result.CommitAfter
		stage := result.FailedStage
		if !stage.Valid() || stage == "" {
			stage = model.StageInternal
		}
		summary := result.Summary
		if summary == "" {
			summary = runErr.Error()
		}
		if result.ResolvedBranch != "" {
			repo.ResolvedBranch = result.ResolvedBranch
			if err := m.repos.Update(context.Background(), repo); err != nil {
				m.logger.Warn("could not update resolved branch", "repo_id", repo.ID, "error", err)
			}
		}
		m.finishFailure(ctx, a, stage, summary, live)
		return
	}
	a.Status = model.ActivitySuccess
	a.Action = result.Action
	a.FailedStage = ""
	a.Summary = result.Summary
	a.CommitBefore = result.CommitBefore
	a.CommitAfter = result.CommitAfter
	if err := live.Close(); err != nil {
		a.Status = model.ActivityFailed
		a.FailedStage = model.StageInternal
		a.Summary = "cannot close log file: " + err.Error()
	}
	finished := time.Now().UTC()
	a.FinishedAt = &finished
	if err := m.activities.Update(context.Background(), &a); err != nil {
		m.logger.Error("could not persist finished activity", "activity_id", a.ID, "error", err)
	}
	if a.Status == model.ActivitySuccess && result.ResolvedBranch != "" {
		repo.ResolvedBranch = result.ResolvedBranch
		if err := m.repos.Update(context.Background(), repo); err != nil {
			m.logger.Warn("could not update resolved branch", "repo_id", repo.ID, "error", err)
		}
	}
	m.afterFinish(a, repo)
}

func (m *Manager) interruptQueued(a model.Activity) {
	a.Status = model.ActivityInterrupted
	a.Summary = "Interrupted by shutdown"
	now := time.Now().UTC()
	a.FinishedAt = &now
	if err := m.logs.Append(a.RepositoryID, a.ID, "\n[gitsync] interrupted: server shutdown\n"); err != nil {
		m.logger.Warn("could not append shutdown marker", "activity_id", a.ID, "error", err)
	}
	if err := m.activities.Update(context.Background(), &a); err != nil {
		m.logger.Error("could not persist interrupted activity", "activity_id", a.ID, "error", err)
	}
	repo, _ := m.repos.Get(context.Background(), a.RepositoryID)
	m.afterFinish(a, repo)
}

func (m *Manager) finishInterrupted(a model.Activity, summary string, live *LiveLog, repo *model.Repository) {
	if live != nil {
		_, _ = fmt.Fprintln(live, "\n[gitsync] interrupted: server shutdown")
		if err := live.Close(); err != nil {
			m.logger.Warn("could not close interrupted activity log", "activity_id", a.ID, "error", err)
		}
	}
	a.Status = model.ActivityInterrupted
	a.Summary = summary
	now := time.Now().UTC()
	a.FinishedAt = &now
	if err := m.activities.Update(context.Background(), &a); err != nil {
		m.logger.Error("could not persist interrupted activity", "activity_id", a.ID, "error", err)
	}
	m.afterFinish(a, repo)
}

func (m *Manager) finishFailure(ctx context.Context, a model.Activity, stage model.Stage, summary string, live *LiveLog) {
	if live != nil {
		if _, err := fmt.Fprintf(live, "[gitsync] failed: %s\n", summary); err != nil {
			m.logger.Warn("could not append failure to activity log", "activity_id", a.ID, "error", err)
		}
		if err := live.Close(); err != nil {
			m.logger.Warn("could not close activity log", "activity_id", a.ID, "error", err)
		}
	}
	a.Status = model.ActivityFailed
	a.FailedStage = stage
	a.Summary = summary
	finished := time.Now().UTC()
	a.FinishedAt = &finished
	if err := m.activities.Update(context.Background(), &a); err != nil {
		m.logger.Error("could not persist failed activity", "activity_id", a.ID, "error", err)
	}
	repo, _ := m.repos.Get(context.Background(), a.RepositoryID)
	m.afterFinish(a, repo)
}

func (m *Manager) failInternal(a model.Activity, summary string) {
	if writer, err := m.logs.Create(a.RepositoryID, a.ID); err == nil {
		_, _ = fmt.Fprintf(writer, "[gitsync] failed: %s\n", summary)
		_ = writer.Close()
	}
	m.finishFailure(context.Background(), a, model.StageInternal, summary, nil)
}

func (m *Manager) afterFinish(a model.Activity, repo *model.Repository) {
	if m.cleaner != nil {
		if _, err := m.cleaner.PruneRepository(context.Background(), a.RepositoryID); err != nil {
			m.logger.Warn("activity cleanup failed", "repo_id", a.RepositoryID, "error", err)
		}
	}
	m.publish("activity.finished", a)
	if (a.Status == model.ActivityFailed || a.Status == model.ActivityInterrupted) && m.notifier != nil && repo != nil {
		m.notifier.Notify(context.Background(), a, *repo)
	}
	m.mu.Lock()
	delete(m.busy, a.RepositoryID)
	m.mu.Unlock()
	if m.LiveLog(a.ID) != nil {
		time.AfterFunc(30*time.Second, func() { m.mu.Lock(); delete(m.live, a.ID); m.mu.Unlock() })
	}
}

// Recover marks all stale queued/running activities interrupted and preserves logs.
func (m *Manager) Recover(ctx context.Context) error {
	active, err := m.activities.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("syncer: list active activities: %w", err)
	}
	for _, a := range active {
		if err := m.logs.Append(a.RepositoryID, a.ID, "\n[gitsync] interrupted: server restarted\n"); err != nil {
			return fmt.Errorf("syncer: append restart marker: %w", err)
		}
		a.Status = model.ActivityInterrupted
		a.Summary = "Interrupted by restart"
		now := time.Now().UTC()
		a.FinishedAt = &now
		if err := m.activities.Update(ctx, &a); err != nil {
			return fmt.Errorf("syncer: mark stale activity interrupted: %w", err)
		}
		repo, _ := m.repos.Get(ctx, a.RepositoryID)
		m.publish("activity.finished", a)
		if m.notifier != nil && repo != nil {
			m.notifier.Notify(ctx, a, *repo)
		}
	}
	return nil
}

// Shutdown rejects new work, waits until ctx expires, then cancels active workers.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("syncer: shutdown context is nil")
	}
	m.mu.Lock()
	m.accepting = false
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.tasks.Wait(); close(done) }()
	select {
	case <-done:
		m.cancel()
		workersDone := make(chan struct{})
		go func() { m.workers.Wait(); close(workersDone) }()
		<-workersDone
		return nil
	case <-ctx.Done():
	}
	m.cancel()
	workersDone := make(chan struct{})
	go func() { m.workers.Wait(); close(workersDone) }()
	<-workersDone
	return m.interruptRemaining()
}

func (m *Manager) interruptRemaining() error {
	for {
		select {
		case a := <-m.jobs:
			m.interruptQueued(a)
			m.tasks.Done()
		default:
			return nil
		}
	}
}

var _ Cleaner = (*history.Cleaner)(nil)
