// Package storetest contains the shared persistence contract suite.
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

type Factory func(*testing.T) (store.Repositories, store.Activities, error)

const (
	repositoryIDOne = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	repositoryIDTwo = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// RunSuite runs the same behavioral checks against each store implementation.
func RunSuite(t *testing.T, factory Factory) {
	t.Helper()
	newStores := func(t *testing.T) (store.Repositories, store.Activities) {
		t.Helper()
		r, a, err := factory(t)
		if err != nil {
			t.Fatal(err)
		}
		if closer, ok := r.(interface{ Close() error }); ok {
			t.Cleanup(func() {
				if err := closer.Close(); err != nil {
					t.Errorf("close repository store: %v", err)
				}
			})
		}
		return r, a
	}
	t.Run("repository CRUD and uniqueness", func(t *testing.T) {
		r, _ := newStores(t)
		ctx := context.Background()
		first := repository("Alpha")
		if err := r.Create(ctx, &first); err != nil {
			t.Fatal(err)
		}
		if first.ID == "" || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
			t.Fatalf("Create did not initialize metadata: %+v", first)
		}
		got, err := r.Get(ctx, first.ID)
		if err != nil || got.Name != "Alpha" {
			t.Fatalf("Get = %+v, %v", got, err)
		}
		got, err = r.GetByName(ctx, " alpha ")
		if err != nil || got.ID != first.ID {
			t.Fatalf("case-insensitive GetByName = %+v, %v", got, err)
		}
		conflict := repository("ALPHA")
		if err = r.Create(ctx, &conflict); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate name error = %v, want conflict", err)
		}
		second := repository("Beta")
		if err = r.Create(ctx, &second); err != nil {
			t.Fatal(err)
		}
		first.Name = "BETA"
		if err = r.Update(ctx, &first); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("conflicting rename = %v, want conflict", err)
		}
		got, err = r.Get(ctx, first.ID)
		if err != nil || got.Name != "Alpha" {
			t.Fatalf("conflicting rename changed state: %+v, %v", got, err)
		}
		first.Name = " Gamma "
		if err = r.Update(ctx, &first); err != nil {
			t.Fatal(err)
		}
		if err = r.Update(ctx, &first); err != nil {
			t.Fatalf("update keeping own name: %v", err)
		}
		if first.Name != "Gamma" {
			t.Fatalf("rename was not trimmed: %q", first.Name)
		}
		if _, err = r.GetByName(ctx, "alpha"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("old name lookup error = %v", err)
		}
		if got, err = r.GetByName(ctx, "gamma"); err != nil || got.ID != first.ID {
			t.Fatalf("new name lookup = %+v, %v", got, err)
		}
		all, err := r.List(ctx)
		if err != nil || len(all) != 2 {
			t.Fatalf("List len=%d, err=%v", len(all), err)
		}
		if err = r.Delete(ctx, first.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = r.Get(ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted repository error = %v", err)
		}
	})
	t.Run("reject malformed IDs before index queries or deletes", func(t *testing.T) {
		r, a := newStores(t)
		ctx := context.Background()
		badRepo := repository("Malformed")
		badRepo.ID = "a"
		if err := r.Create(ctx, &badRepo); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("malformed repository ID error = %v, want ErrInvalid", err)
		}
		valid := activity(repositoryIDOne, model.ActivityQueued, time.Now().UTC())
		if err := a.Create(ctx, &valid); err != nil {
			t.Fatal(err)
		}
		badRepositoryID := activity("a\x00b", model.ActivityQueued, time.Now().UTC())
		if err := a.Create(ctx, &badRepositoryID); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("delimiter-bearing repository ID error = %v, want ErrInvalid", err)
		}
		badActivityID := activity(repositoryIDOne, model.ActivityQueued, time.Now().UTC())
		badActivityID.ID = "not-a-uuid"
		if err := a.Create(ctx, &badActivityID); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("malformed activity ID error = %v, want ErrInvalid", err)
		}
		if _, err := a.ListByRepository(ctx, "a", 0, nil); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("malformed list repository ID error = %v, want ErrInvalid", err)
		}
		if _, err := a.DeleteByRepository(ctx, "a"); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("malformed delete repository ID error = %v, want ErrInvalid", err)
		}
		if _, err := a.Get(ctx, valid.ID); err != nil {
			t.Fatalf("valid indexed activity was affected: %v", err)
		}
		ids, err := a.DeleteByRepository(ctx, repositoryIDOne)
		if err != nil || len(ids) != 1 || ids[0] != valid.ID {
			t.Fatalf("valid repository delete = %v, %v", ids, err)
		}
	})
	t.Run("activity CRUD, ordering, paging, active and retention", func(t *testing.T) {
		_, a := newStores(t)
		ctx := context.Background()
		base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
		statuses := []model.ActivityStatus{model.ActivitySuccess, model.ActivityRunning, model.ActivityFailed, model.ActivityQueued, model.ActivityInterrupted}
		items := make([]model.Activity, len(statuses))
		for i, status := range statuses {
			items[i] = activity(repositoryIDOne, status, base.Add(time.Duration(i)*time.Second))
			if err := a.Create(ctx, &items[i]); err != nil {
				t.Fatal(err)
			}
		}
		got, err := a.Get(ctx, items[0].ID)
		if err != nil || got.Status != model.ActivitySuccess {
			t.Fatalf("Get = %+v, %v", got, err)
		}
		got.Status = model.ActivityRunning
		if err = a.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		list, err := a.ListByRepository(ctx, repositoryIDOne, 0, nil)
		if err != nil || len(list) != 5 {
			t.Fatalf("ListByRepository len=%d err=%v", len(list), err)
		}
		if list[0].ID != items[4].ID || list[4].ID != items[0].ID {
			t.Fatalf("newest-first order = %v .. %v", list[0].ID, list[4].ID)
		}
		before := base.Add(4 * time.Second)
		page, err := a.ListByRepository(ctx, repositoryIDOne, 2, &before)
		if err != nil || len(page) != 2 || page[0].ID != items[3].ID {
			t.Fatalf("paged result = %+v, %v", page, err)
		}
		recent, err := a.ListRecent(ctx, 2)
		if err != nil || len(recent) != 2 || recent[0].ID != items[4].ID {
			t.Fatalf("ListRecent = %+v, %v", recent, err)
		}
		active, err := a.ListActive(ctx)
		if err != nil || len(active) != 3 {
			t.Fatalf("ListActive len=%d, err=%v", len(active), err)
		}
		latest, err := a.LatestFinishedPerRepository(ctx)
		if err != nil || len(latest) != 1 || latest[0].ID != items[4].ID {
			t.Fatalf("LatestFinishedPerRepository = %+v, %v", latest, err)
		}
		// Restore the first activity to a finished state and create extra finished records for retention cases.
		got.Status = model.ActivitySuccess
		if err = a.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		active, err = a.ListActive(ctx)
		if err != nil || len(active) != 2 {
			t.Fatalf("finished transition left active index entry: count=%d err=%v", len(active), err)
		}
		for i := 0; i < 3; i++ {
			v := activity(repositoryIDOne, model.ActivitySuccess, base.Add(time.Duration(10+i)*time.Second))
			if err = a.Create(ctx, &v); err != nil {
				t.Fatal(err)
			}
		}
		for _, tc := range []struct{ keep, want int }{{0, 6}, {1, 5}, {2, 4}, {20, 0}} {
			beyond, err := a.ListFinishedBeyond(ctx, repositoryIDOne, tc.keep)
			if err != nil || len(beyond) != tc.want {
				t.Errorf("ListFinishedBeyond(%d) len=%d, err=%v, want %d", tc.keep, len(beyond), err, tc.want)
			}
			if len(beyond) > 1 && !beyond[0].CreatedAt.Before(beyond[len(beyond)-1].CreatedAt) {
				t.Errorf("excess records are not oldest-first: %+v", beyond)
			}
		}
		missing := uuid.NewString()
		if err = a.DeleteMany(ctx, []string{items[0].ID, missing, items[0].ID}); err != nil {
			t.Fatal(err)
		}
		if _, err = a.Get(ctx, items[0].ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted activity error = %v", err)
		}
		ids, err := a.DeleteByRepository(ctx, repositoryIDOne)
		if err != nil || len(ids) != 7 {
			t.Fatalf("DeleteByRepository ids=%d err=%v", len(ids), err)
		}
	})
	t.Run("parallel operations", func(t *testing.T) {
		_, a := newStores(t)
		ctx := context.Background()
		const count = 24
		var wg sync.WaitGroup
		errs := make(chan error, count)
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				v := activity(repositoryIDTwo, model.ActivityQueued, time.Now().UTC().Add(time.Duration(i)*time.Nanosecond))
				if err := a.Create(ctx, &v); err != nil {
					errs <- err
					return
				}
				stored, err := a.Get(ctx, v.ID)
				if err != nil {
					errs <- err
					return
				}
				stored.Status = model.ActivityRunning
				if err = a.Update(ctx, stored); err != nil {
					errs <- err
					return
				}
				if _, err = a.ListByRepository(ctx, repositoryIDTwo, 10, nil); err != nil {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		active, err := a.ListActive(ctx)
		if err != nil || len(active) != count {
			t.Fatalf("parallel active count=%d err=%v", len(active), err)
		}
	})
}

func repository(name string) model.Repository {
	return model.Repository{ID: uuid.NewString(), Name: name, URL: "https://example.invalid/repo.git", Path: "/tmp/repo"}
}
func activity(repo string, status model.ActivityStatus, created time.Time) model.Activity {
	return model.Activity{ID: uuid.NewString(), RepositoryID: repo, RepositoryName: repo, Status: status, Trigger: model.TriggerManual, Source: model.SourceManual, CreatedAt: created}
}
