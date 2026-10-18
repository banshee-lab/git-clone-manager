// Package bolt implements the GitSync stores using bbolt and explicit indexes.
package bolt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gitsync/internal/model"
	"gitsync/internal/store"
	bbolt "go.etcd.io/bbolt"
)

const schemaVersion = 1

var (
	bucketMeta       = []byte("meta")
	bucketRepos      = []byte("repositories")
	bucketRepoNames  = []byte("repo_names")
	bucketActivities = []byte("activities")
	bucketActByRepo  = []byte("act_by_repo")
	bucketActByTime  = []byte("act_by_time")
	bucketActActive  = []byte("act_active")
)

type DB struct{ db *bbolt.DB }

// Open creates or opens path with a five-second exclusive-lock timeout.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open database %q (only one GitSync instance may use a data_dir): %w", path, err)
	}
	out := &DB{db: db}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{bucketMeta, bucketRepos, bucketRepoNames, bucketActivities, bucketActByRepo, bucketActByTime, bucketActActive} {
			if _, e := tx.CreateBucketIfNotExists(name); e != nil {
				return e
			}
		}
		meta := tx.Bucket(bucketMeta)
		v := meta.Get([]byte("schema_version"))
		if v == nil {
			return meta.Put([]byte("schema_version"), []byte("1"))
		}
		var version int
		if _, e := fmt.Sscanf(string(v), "%d", &version); e != nil {
			return fmt.Errorf("invalid database schema version %q: %w", v, e)
		}
		if version > schemaVersion {
			return fmt.Errorf("database schema version %d is newer than supported version %d", version, schemaVersion)
		}
		if version != schemaVersion {
			return fmt.Errorf("unsupported database schema version %d (supported: %d)", version, schemaVersion)
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	return out, nil
}

func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}
func contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	return ctx.Err()
}
func encode(v any) ([]byte, error) { return json.Marshal(v) }
func decode(data []byte, dst any) error {
	if data == nil {
		return store.ErrNotFound
	}
	return json.Unmarshal(data, dst)
}
func nameKey(name string) []byte { return []byte(strings.ToLower(strings.TrimSpace(name))) }
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
func activityRepoKey(a model.Activity) []byte {
	b := make([]byte, len(a.RepositoryID)+1+8+len(a.ID))
	copy(b, a.RepositoryID)
	off := len(a.RepositoryID)
	b[off] = 0
	binary.BigEndian.PutUint64(b[off+1:], uint64(a.CreatedAt.UnixNano()))
	copy(b[off+9:], a.ID)
	return b
}
func activityTimeKey(a model.Activity) []byte {
	b := make([]byte, 8+len(a.ID))
	binary.BigEndian.PutUint64(b, uint64(a.CreatedAt.UnixNano()))
	copy(b[8:], a.ID)
	return b
}
func active(s model.ActivityStatus) bool {
	return s == model.ActivityQueued || s == model.ActivityRunning
}

func (d *DB) CreateRepository(ctx context.Context, r *model.Repository) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("nil repository")
	}
	v := *r
	initRepo(&v)
	data, err := encode(v)
	if err != nil {
		return err
	}
	err = d.db.Update(func(tx *bbolt.Tx) error {
		repos, names := tx.Bucket(bucketRepos), tx.Bucket(bucketRepoNames)
		if repos.Get([]byte(v.ID)) != nil {
			return store.ErrConflict
		}
		if names.Get(nameKey(v.Name)) != nil {
			return store.ErrConflict
		}
		if e := repos.Put([]byte(v.ID), data); e != nil {
			return e
		}
		if e := names.Put(nameKey(v.Name), []byte(v.ID)); e != nil {
			return e
		}
		return nil
	})
	if err == nil {
		*r = v
	}
	return err
}
func (d *DB) GetRepository(ctx context.Context, id string) (*model.Repository, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	var v model.Repository
	err := d.db.View(func(tx *bbolt.Tx) error { return decode(tx.Bucket(bucketRepos).Get([]byte(id)), &v) })
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func (d *DB) GetRepositoryByName(ctx context.Context, name string) (*model.Repository, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	var v model.Repository
	err := d.db.View(func(tx *bbolt.Tx) error {
		id := tx.Bucket(bucketRepoNames).Get(nameKey(name))
		if id == nil {
			return store.ErrNotFound
		}
		return decode(tx.Bucket(bucketRepos).Get(id), &v)
	})
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func (d *DB) ListRepositories(ctx context.Context) ([]model.Repository, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	out := []model.Repository{}
	err := d.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketRepos).ForEach(func(_, v []byte) error {
			var r model.Repository
			if e := decode(v, &r); e != nil {
				return e
			}
			out = append(out, r)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, err
}
func (d *DB) UpdateRepository(ctx context.Context, r *model.Repository) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if r == nil {
		return errors.New("nil repository")
	}
	v := *r
	v.Name = strings.TrimSpace(v.Name)
	v.UpdatedAt = time.Now().UTC()
	err := d.db.Update(func(tx *bbolt.Tx) error {
		repos, names := tx.Bucket(bucketRepos), tx.Bucket(bucketRepoNames)
		var old model.Repository
		if e := decode(repos.Get([]byte(v.ID)), &old); e != nil {
			return e
		}
		owner := names.Get(nameKey(v.Name))
		if owner != nil && !bytes.Equal(owner, []byte(v.ID)) {
			return store.ErrConflict
		}
		v.CreatedAt = old.CreatedAt
		data, e := encode(v)
		if e != nil {
			return e
		}
		if !bytes.Equal(nameKey(old.Name), nameKey(v.Name)) {
			if e = names.Delete(nameKey(old.Name)); e != nil {
				return e
			}
			if e = names.Put(nameKey(v.Name), []byte(v.ID)); e != nil {
				return e
			}
		}
		return repos.Put([]byte(v.ID), data)
	})
	if err == nil {
		*r = v
	}
	return err
}
func (d *DB) DeleteRepository(ctx context.Context, id string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return d.db.Update(func(tx *bbolt.Tx) error {
		repos, names := tx.Bucket(bucketRepos), tx.Bucket(bucketRepoNames)
		var v model.Repository
		if e := decode(repos.Get([]byte(id)), &v); e != nil {
			return e
		}
		if e := names.Delete(nameKey(v.Name)); e != nil {
			return e
		}
		return repos.Delete([]byte(id))
	})
}

func (d *DB) CreateActivity(ctx context.Context, a *model.Activity) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if a == nil {
		return errors.New("nil activity")
	}
	v := *a
	initActivity(&v)
	data, err := encode(v)
	if err != nil {
		return err
	}
	err = d.db.Update(func(tx *bbolt.Tx) error {
		records := tx.Bucket(bucketActivities)
		if records.Get([]byte(v.ID)) != nil {
			return store.ErrConflict
		}
		if e := records.Put([]byte(v.ID), data); e != nil {
			return e
		}
		if e := tx.Bucket(bucketActByRepo).Put(activityRepoKey(v), []byte(v.ID)); e != nil {
			return e
		}
		if e := tx.Bucket(bucketActByTime).Put(activityTimeKey(v), []byte(v.ID)); e != nil {
			return e
		}
		if active(v.Status) {
			return tx.Bucket(bucketActActive).Put([]byte(v.ID), []byte{1})
		}
		return nil
	})
	if err == nil {
		*a = v
	}
	return err
}
func (d *DB) GetActivity(ctx context.Context, id string) (*model.Activity, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	var a model.Activity
	err := d.db.View(func(tx *bbolt.Tx) error { return decode(tx.Bucket(bucketActivities).Get([]byte(id)), &a) })
	if err != nil {
		return nil, err
	}
	return &a, nil
}
func (d *DB) UpdateActivity(ctx context.Context, a *model.Activity) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if a == nil {
		return errors.New("nil activity")
	}
	v := *a
	if v.StartedAt != nil {
		t := v.StartedAt.UTC()
		v.StartedAt = &t
	}
	if v.FinishedAt != nil {
		t := v.FinishedAt.UTC()
		v.FinishedAt = &t
	}
	err := d.db.Update(func(tx *bbolt.Tx) error {
		records := tx.Bucket(bucketActivities)
		var old model.Activity
		if e := decode(records.Get([]byte(v.ID)), &old); e != nil {
			return e
		}
		v.CreatedAt = old.CreatedAt
		if e := tx.Bucket(bucketActByRepo).Delete(activityRepoKey(old)); e != nil {
			return e
		}
		if e := tx.Bucket(bucketActByTime).Delete(activityTimeKey(old)); e != nil {
			return e
		}
		activeBucket := tx.Bucket(bucketActActive)
		if active(old.Status) && !active(v.Status) {
			if e := activeBucket.Delete([]byte(v.ID)); e != nil {
				return e
			}
		} else if !active(old.Status) && active(v.Status) {
			if e := activeBucket.Put([]byte(v.ID), []byte{1}); e != nil {
				return e
			}
		}
		data, e := encode(v)
		if e != nil {
			return e
		}
		if e = records.Put([]byte(v.ID), data); e != nil {
			return e
		}
		if e = tx.Bucket(bucketActByRepo).Put(activityRepoKey(v), []byte(v.ID)); e != nil {
			return e
		}
		return tx.Bucket(bucketActByTime).Put(activityTimeKey(v), []byte(v.ID))
	})
	if err == nil {
		*a = v
	}
	return err
}

func readActivity(tx *bbolt.Tx, id []byte) (model.Activity, error) {
	var a model.Activity
	err := decode(tx.Bucket(bucketActivities).Get(id), &a)
	return a, err
}
func (d *DB) ListByRepository(ctx context.Context, id string, limit int, before *time.Time) ([]model.Activity, error) {
	return d.listIndex(ctx, bucketActByRepo, []byte(id+"\x00"), limit, before, true, nil)
}
func (d *DB) ListRecent(ctx context.Context, limit int) ([]model.Activity, error) {
	return d.listIndex(ctx, bucketActByTime, nil, limit, nil, true, nil)
}
func (d *DB) ListActive(ctx context.Context) ([]model.Activity, error) {
	out := []model.Activity{}
	err := contextErr(ctx)
	if err != nil {
		return nil, err
	}
	err = d.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketActActive).ForEach(func(k, _ []byte) error {
			a, e := readActivity(tx, k)
			if e != nil {
				return e
			}
			out = append(out, a)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return activityDesc(out[i], out[j]) })
	return out, err
}
func activityDesc(a, b model.Activity) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID > b.ID
	}
	return a.CreatedAt.After(b.CreatedAt)
}
func (d *DB) listIndex(ctx context.Context, bucket, prefix []byte, limit int, before *time.Time, desc bool, filter func(model.Activity) bool) ([]model.Activity, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	out := []model.Activity{}
	err := d.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		var k, v []byte
		if desc {
			k, v = c.Seek(append(append([]byte(nil), prefix...), 0xff))
			if k == nil {
				k, v = c.Last()
			} else if !bytes.HasPrefix(k, prefix) {
				k, v = c.Prev()
			}
			for k != nil && bytes.HasPrefix(k, prefix) {
				a, e := readActivity(tx, v)
				if e != nil {
					return e
				}
				if (before == nil || a.CreatedAt.Before(*before)) && (filter == nil || filter(a)) {
					out = append(out, a)
					if limit > 0 && len(out) >= limit {
						break
					}
				}
				k, v = c.Prev()
			}
		} else {
			for k, v = c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
				a, e := readActivity(tx, v)
				if e != nil {
					return e
				}
				if (before == nil || a.CreatedAt.Before(*before)) && (filter == nil || filter(a)) {
					out = append(out, a)
					if limit > 0 && len(out) >= limit {
						break
					}
				}
			}
		}
		return nil
	})
	return out, err
}
func (d *DB) LatestFinishedPerRepository(ctx context.Context) ([]model.Activity, error) {
	all, err := d.ListRecent(ctx, 0)
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
func (d *DB) ListFinishedBeyond(ctx context.Context, id string, keep int) ([]model.Activity, error) {
	all, err := d.listIndex(ctx, bucketActByRepo, []byte(id+"\x00"), 0, nil, true, func(a model.Activity) bool { return a.Status.IsFinished() })
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
func (d *DB) DeleteMany(ctx context.Context, ids []string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return d.db.Update(func(tx *bbolt.Tx) error {
		for _, id := range ids {
			records := tx.Bucket(bucketActivities)
			raw := records.Get([]byte(id))
			if raw == nil {
				continue
			}
			var a model.Activity
			if e := decode(raw, &a); e != nil {
				return e
			}
			if e := records.Delete([]byte(id)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActByRepo).Delete(activityRepoKey(a)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActByTime).Delete(activityTimeKey(a)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActActive).Delete([]byte(id)); e != nil {
				return e
			}
		}
		return nil
	})
}
func (d *DB) DeleteByRepository(ctx context.Context, id string) ([]string, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	ids := []string{}
	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketActByRepo)
		prefix := append([]byte(id), 0)
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			ids = append(ids, string(append([]byte(nil), v...)))
		}
		for _, key := range ids {
			raw := tx.Bucket(bucketActivities).Get([]byte(key))
			if raw == nil {
				return fmt.Errorf("activity index points to missing record %q", key)
			}
			var a model.Activity
			if e := decode(raw, &a); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActByRepo).Delete(activityRepoKey(a)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActByTime).Delete(activityTimeKey(a)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActActive).Delete([]byte(key)); e != nil {
				return e
			}
			if e := tx.Bucket(bucketActivities).Delete([]byte(key)); e != nil {
				return e
			}
		}
		return nil
	})
	sort.Strings(ids)
	return ids, err
}

// RepositoryStore and ActivityStore adapt the shared DB to the independent interfaces.
type RepositoryStore struct{ db *DB }
type ActivityStore struct{ db *DB }

func (d *DB) Repositories() store.Repositories { return RepositoryStore{db: d} }
func (d *DB) Activities() store.Activities     { return ActivityStore{db: d} }
func (s RepositoryStore) Create(c context.Context, r *model.Repository) error {
	return s.db.CreateRepository(c, r)
}
func (s RepositoryStore) Get(c context.Context, id string) (*model.Repository, error) {
	return s.db.GetRepository(c, id)
}
func (s RepositoryStore) GetByName(c context.Context, n string) (*model.Repository, error) {
	return s.db.GetRepositoryByName(c, n)
}
func (s RepositoryStore) List(c context.Context) ([]model.Repository, error) {
	return s.db.ListRepositories(c)
}
func (s RepositoryStore) Update(c context.Context, r *model.Repository) error {
	return s.db.UpdateRepository(c, r)
}
func (s RepositoryStore) Delete(c context.Context, id string) error {
	return s.db.DeleteRepository(c, id)
}
func (s ActivityStore) Create(c context.Context, a *model.Activity) error {
	return s.db.CreateActivity(c, a)
}
func (s ActivityStore) Get(c context.Context, id string) (*model.Activity, error) {
	return s.db.GetActivity(c, id)
}
func (s ActivityStore) Update(c context.Context, a *model.Activity) error {
	return s.db.UpdateActivity(c, a)
}
func (s ActivityStore) ListByRepository(c context.Context, id string, l int, b *time.Time) ([]model.Activity, error) {
	return s.db.ListByRepository(c, id, l, b)
}
func (s ActivityStore) ListRecent(c context.Context, l int) ([]model.Activity, error) {
	return s.db.ListRecent(c, l)
}
func (s ActivityStore) ListActive(c context.Context) ([]model.Activity, error) {
	return s.db.ListActive(c)
}
func (s ActivityStore) LatestFinishedPerRepository(c context.Context) ([]model.Activity, error) {
	return s.db.LatestFinishedPerRepository(c)
}
func (s ActivityStore) ListFinishedBeyond(c context.Context, id string, k int) ([]model.Activity, error) {
	return s.db.ListFinishedBeyond(c, id, k)
}
func (s ActivityStore) DeleteMany(c context.Context, ids []string) error {
	return s.db.DeleteMany(c, ids)
}
func (s ActivityStore) DeleteByRepository(c context.Context, id string) ([]string, error) {
	return s.db.DeleteByRepository(c, id)
}
