package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/model"
)

// Memory is a concurrency-safe in-memory implementation intended for tests.
type Memory struct {
	mu         sync.RWMutex
	repos      map[string]model.Repository
	activities map[string]model.Activity
	closed     bool
}

type MemoryRepositories struct{ *Memory }
type MemoryActivities struct{ *Memory }

func NewMemory() (*MemoryRepositories, *MemoryActivities) {
	m := &Memory{repos: make(map[string]model.Repository), activities: make(map[string]model.Activity)}
	return &MemoryRepositories{m}, &MemoryActivities{m}
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
func (m *Memory) check() error {
	if m.closed {
		return ErrClosed
	}
	return nil
}
func repoName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func initRepo(r *model.Repository) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.Name = strings.TrimSpace(r.Name)
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = now
}
func initActivity(a *model.Activity) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	a.CreatedAt = a.CreatedAt.UTC()
	if a.StartedAt != nil {
		v := a.StartedAt.UTC()
		a.StartedAt = &v
	}
	if a.FinishedAt != nil {
		v := a.FinishedAt.UTC()
		a.FinishedAt = &v
	}
}
func (m *MemoryRepositories) Create(ctx context.Context, r *model.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return errors.New("nil repository")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	copy := *r
	initRepo(&copy)
	if err := ValidateUUIDv4ID(copy.ID); err != nil {
		return err
	}
	if _, ok := m.repos[copy.ID]; ok {
		return ErrConflict
	}
	for _, old := range m.repos {
		if repoName(old.Name) == repoName(copy.Name) {
			return ErrConflict
		}
	}
	m.repos[copy.ID] = copy
	*r = copy
	return nil
}
func (m *MemoryRepositories) Get(ctx context.Context, id string) (*model.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateUUIDv4ID(id); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	r, ok := m.repos[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}
func (m *MemoryRepositories) GetByName(ctx context.Context, name string) (*model.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	for _, r := range m.repos {
		if repoName(r.Name) == repoName(name) {
			return &r, nil
		}
	}
	return nil, ErrNotFound
}
func (m *MemoryRepositories) List(ctx context.Context) ([]model.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	out := make([]model.Repository, 0, len(m.repos))
	for _, r := range m.repos {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
func (m *MemoryRepositories) Update(ctx context.Context, r *model.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return errors.New("nil repository")
	}
	if err := ValidateUUIDv4ID(r.ID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	old, ok := m.repos[r.ID]
	if !ok {
		return ErrNotFound
	}
	copy := *r
	copy.Name = strings.TrimSpace(copy.Name)
	copy.CreatedAt = old.CreatedAt
	copy.UpdatedAt = time.Now().UTC()
	for id, other := range m.repos {
		if id != copy.ID && repoName(other.Name) == repoName(copy.Name) {
			return ErrConflict
		}
	}
	m.repos[copy.ID] = copy
	*r = copy
	return nil
}
func (m *MemoryRepositories) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateUUIDv4ID(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	if _, ok := m.repos[id]; !ok {
		return ErrNotFound
	}
	delete(m.repos, id)
	return nil
}
func (m *MemoryActivities) Create(ctx context.Context, a *model.Activity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil {
		return errors.New("nil activity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	copy := *a
	initActivity(&copy)
	if err := ValidateUUIDv4ID(copy.ID); err != nil {
		return err
	}
	if err := ValidateUUIDv4ID(copy.RepositoryID); err != nil {
		return err
	}
	if _, ok := m.activities[copy.ID]; ok {
		return ErrConflict
	}
	m.activities[copy.ID] = copy
	*a = copy
	return nil
}
func (m *MemoryActivities) getActivity(ctx context.Context, id string) (*model.Activity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateUUIDv4ID(id); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	a, ok := m.activities[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneActivity(a), nil
}
func (m *MemoryActivities) Get(ctx context.Context, id string) (*model.Activity, error) {
	return m.getActivity(ctx, id)
}
func cloneActivity(a model.Activity) *model.Activity {
	if a.StartedAt != nil {
		t := *a.StartedAt
		a.StartedAt = &t
	}
	if a.FinishedAt != nil {
		t := *a.FinishedAt
		a.FinishedAt = &t
	}
	return &a
}
func (m *MemoryActivities) Update(ctx context.Context, a *model.Activity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil {
		return errors.New("nil activity")
	}
	if err := ValidateUUIDv4ID(a.ID); err != nil {
		return err
	}
	if err := ValidateUUIDv4ID(a.RepositoryID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	old, ok := m.activities[a.ID]
	if !ok {
		return ErrNotFound
	}
	copy := *a
	copy.CreatedAt = old.CreatedAt
	if copy.StartedAt != nil {
		v := copy.StartedAt.UTC()
		copy.StartedAt = &v
	}
	if copy.FinishedAt != nil {
		v := copy.FinishedAt.UTC()
		copy.FinishedAt = &v
	}
	m.activities[a.ID] = copy
	*a = copy
	return nil
}
func activityDesc(a, b model.Activity) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID > b.ID
	}
	return a.CreatedAt.After(b.CreatedAt)
}
func (m *Memory) activityList(ctx context.Context, pred func(model.Activity) bool, limit int, before *time.Time) ([]model.Activity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	out := make([]model.Activity, 0)
	for _, a := range m.activities {
		if pred(a) && (before == nil || a.CreatedAt.Before(*before)) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return activityDesc(out[i], out[j]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		if out[i].StartedAt != nil {
			t := *out[i].StartedAt
			out[i].StartedAt = &t
		}
		if out[i].FinishedAt != nil {
			t := *out[i].FinishedAt
			out[i].FinishedAt = &t
		}
	}
	return out, nil
}
func (m *MemoryActivities) ListByRepository(ctx context.Context, id string, limit int, before *time.Time) ([]model.Activity, error) {
	if err := ValidateUUIDv4ID(id); err != nil {
		return nil, err
	}
	return m.activityList(ctx, func(a model.Activity) bool { return a.RepositoryID == id }, limit, before)
}
func (m *MemoryActivities) ListRecent(ctx context.Context, limit int) ([]model.Activity, error) {
	return m.activityList(ctx, func(model.Activity) bool { return true }, limit, nil)
}
func (m *MemoryActivities) ListActive(ctx context.Context) ([]model.Activity, error) {
	return m.activityList(ctx, func(a model.Activity) bool {
		return a.Status == model.ActivityQueued || a.Status == model.ActivityRunning
	}, 0, nil)
}
func (m *MemoryActivities) LatestFinishedPerRepository(ctx context.Context) ([]model.Activity, error) {
	all, err := m.ListRecent(ctx, 0)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []model.Activity{}
	for _, a := range all {
		if a.Status.IsFinished() && !seen[a.RepositoryID] {
			seen[a.RepositoryID] = true
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *MemoryActivities) ListFinishedBeyond(ctx context.Context, id string, keep int) ([]model.Activity, error) {
	if err := ValidateUUIDv4ID(id); err != nil {
		return nil, err
	}
	all, err := m.activityList(ctx, func(a model.Activity) bool { return a.RepositoryID == id && a.Status.IsFinished() }, 0, nil)
	if err != nil {
		return nil, err
	}
	if keep < 0 {
		keep = 0
	}
	if len(all) <= keep {
		return []model.Activity{}, nil
	}
	out := append([]model.Activity(nil), all[keep:]...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
func (m *MemoryActivities) DeleteMany(ctx context.Context, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := ValidateUUIDv4ID(id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return err
	}
	for _, id := range ids {
		delete(m.activities, id)
	}
	return nil
}
func (m *MemoryActivities) DeleteByRepository(ctx context.Context, id string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateUUIDv4ID(id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(); err != nil {
		return nil, err
	}
	ids := []string{}
	for key, a := range m.activities {
		if a.RepositoryID == id {
			ids = append(ids, key)
			delete(m.activities, key)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
