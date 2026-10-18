package bolt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitsync/internal/model"
	"gitsync/internal/store"
	"gitsync/internal/store/storetest"
	bbolt "go.etcd.io/bbolt"
)

func TestBoltStoreContract(t *testing.T) {
	storetest.RunSuite(t, func(t *testing.T) (store.Repositories, store.Activities, error) {
		db, err := Open(filepath.Join(t.TempDir(), "gitsync.db"))
		if err != nil {
			return nil, nil, err
		}
		t.Cleanup(func() { _ = db.Close() })
		return db.Repositories(), db.Activities(), nil
	})
}

func TestBoltPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitsync.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := db.Repositories()
	a := db.Activities()
	repo := model.Repository{Name: "Persisted", URL: "https://example.invalid/repo.git", Path: "/tmp/repo"}
	if err = r.Create(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	act := model.Activity{RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivityRunning, Trigger: model.TriggerManual, Source: model.SourceManual}
	if err = a.Create(context.Background(), &act); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Repositories().Get(context.Background(), repo.ID)
	if err != nil || got.Name != repo.Name {
		t.Fatalf("repo reopen = %+v, %v", got, err)
	}
	gotActivity, err := db.Activities().Get(context.Background(), act.ID)
	if err != nil || gotActivity.Status != model.ActivityRunning {
		t.Fatalf("activity reopen = %+v, %v", gotActivity, err)
	}
}

func TestBoltLockTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitsync.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	started := time.Now()
	_, err = Open(path)
	if err == nil {
		t.Fatal("second Open succeeded while first holds the lock")
	}
	if time.Since(started) > 7*time.Second {
		t.Fatalf("second Open took too long: %s", time.Since(started))
	}
	if !errors.Is(err, bbolt.ErrTimeout) {
		t.Fatalf("second Open error = %v, want bbolt lock timeout", err)
	}
	if !strings.Contains(err.Error(), "one GitSync instance") {
		t.Fatalf("lock error is unclear: %v", err)
	}
}

func TestBoltRejectsHigherSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitsync.db")
	raw, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = raw.Update(func(tx *bbolt.Tx) error {
		b, e := tx.CreateBucket(bucketMeta)
		if e != nil {
			return e
		}
		return b.Put([]byte("schema_version"), []byte("2"))
	}); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); err == nil {
		t.Fatal("Open accepted higher schema version")
	} else if !contains(err.Error(), "newer than supported") {
		t.Fatalf("schema error is unclear: %v", err)
	}
}

func TestBoltRandomizedIndexConsistency(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gitsync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	activities := db.Activities()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(17))
	ids := make([]string, 0)
	statuses := []model.ActivityStatus{model.ActivityQueued, model.ActivityRunning, model.ActivitySuccess, model.ActivityFailed, model.ActivityInterrupted}
	for i := 0; i < 160; i++ {
		if len(ids) > 0 && rng.Intn(5) == 0 {
			index := rng.Intn(len(ids))
			id := ids[index]
			a, e := activities.Get(ctx, id)
			if e == nil {
				a.Status = statuses[rng.Intn(len(statuses))]
				if e = activities.Update(ctx, a); e != nil {
					t.Fatal(e)
				}
			}
			continue
		}
		a := model.Activity{RepositoryID: fmt.Sprintf("repo-%d", rng.Intn(4)), RepositoryName: "repo", Status: statuses[rng.Intn(len(statuses))], Trigger: model.TriggerManual, Source: model.SourceManual, CreatedAt: time.Unix(1_700_000_000+int64(i), 0).UTC()}
		if e := activities.Create(ctx, &a); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, a.ID)
		if len(ids) > 3 && rng.Intn(8) == 0 {
			remove := ids[rng.Intn(len(ids))]
			if e := activities.DeleteMany(ctx, []string{remove}); e != nil {
				t.Fatal(e)
			}
		}
	}
	checkIndexes(t, db)
}

func checkIndexes(t *testing.T, db *DB) {
	t.Helper()
	err := db.db.View(func(tx *bbolt.Tx) error {
		records := map[string]model.Activity{}
		if err := tx.Bucket(bucketActivities).ForEach(func(k, v []byte) error {
			var a model.Activity
			if err := jsonUnmarshal(v, &a); err != nil {
				return err
			}
			records[string(k)] = a
			return nil
		}); err != nil {
			return err
		}
		repoSeen := map[string]bool{}
		if err := tx.Bucket(bucketActByRepo).ForEach(func(k, v []byte) error {
			id := string(v)
			a, ok := records[id]
			if !ok {
				return fmt.Errorf("repo index points to missing %s", id)
			}
			if string(k) != string(activityRepoKey(a)) {
				return fmt.Errorf("bad repo index key for %s", id)
			}
			if repoSeen[id] {
				return fmt.Errorf("duplicate repo index for %s", id)
			}
			repoSeen[id] = true
			return nil
		}); err != nil {
			return err
		}
		timeSeen := map[string]bool{}
		if err := tx.Bucket(bucketActByTime).ForEach(func(k, v []byte) error {
			id := string(v)
			a, ok := records[id]
			if !ok {
				return fmt.Errorf("time index points to missing %s", id)
			}
			if string(k) != string(activityTimeKey(a)) {
				return fmt.Errorf("bad time index key for %s", id)
			}
			if timeSeen[id] {
				return fmt.Errorf("duplicate time index for %s", id)
			}
			timeSeen[id] = true
			return nil
		}); err != nil {
			return err
		}
		active := map[string]bool{}
		if err := tx.Bucket(bucketActActive).ForEach(func(k, _ []byte) error {
			id := string(k)
			a, ok := records[id]
			if !ok {
				return fmt.Errorf("active index points to missing %s", id)
			}
			active[id] = true
			if !isActive(a.Status) {
				return fmt.Errorf("finished activity %s in active index", id)
			}
			return nil
		}); err != nil {
			return err
		}
		for id, a := range records {
			if !repoSeen[id] || !timeSeen[id] {
				return fmt.Errorf("missing index for %s", id)
			}
			if isActive(a.Status) != active[id] {
				return fmt.Errorf("active index mismatch for %s", id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func isActive(s model.ActivityStatus) bool {
	return s == model.ActivityQueued || s == model.ActivityRunning
}
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
func contains(s, sub string) bool            { return strings.Contains(s, sub) }
