package logstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const repoID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const actID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

func newTestStore(t *testing.T) *FS {
	t.Helper()
	s, err := New(t.TempDir() + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateAppendReadAndModes(t *testing.T) {
	s := newTestStore(t)
	w, err := s.Create(repoID, actID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello\nworld")); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Size(); err != nil || n != 11 {
		t.Fatalf("Size = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(repoID, actID, "!"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAll(repoID, actID)
	if err != nil || string(got) != "hello\nworld!" {
		t.Fatalf("ReadAll = %q, %v", got, err)
	}
	path, _ := s.Path(repoID, actID)
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o640 {
		t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o750 {
		t.Fatalf("directory mode = %o", dirInfo.Mode().Perm())
	}
}

func TestReadRangeDoesNotSplitUTF8(t *testing.T) {
	s := newTestStore(t)
	if err := s.Append(repoID, actID, "a🙂b"); err != nil {
		t.Fatal(err)
	}
	b, next, err := s.ReadRange(repoID, actID, 0, 3)
	if err != nil || string(b) != "a" || next != 1 {
		t.Fatalf("first range = %q, %d, %v", b, next, err)
	}
	b, next, err = s.ReadRange(repoID, actID, next, 4)
	if err != nil || string(b) != "🙂" || next != 5 {
		t.Fatalf("emoji range = %q, %d, %v", b, next, err)
	}
	b, next, err = s.ReadRange(repoID, actID, 2, 8)
	if err != nil || string(b) != "b" || next != 6 {
		t.Fatalf("mid-rune range = %q, %d, %v", b, next, err)
	}
}

func TestTail(t *testing.T) {
	s := newTestStore(t)
	for _, tc := range []struct {
		text string
		n    int
		want string
	}{
		{"one\ntwo\nthree", 2, "two\nthree"},
		{"one\ntwo\nthree\n", 2, "two\nthree\n"},
		{"one\ntwo", 8, "one\ntwo"},
		{strings.Repeat("x", 5000) + "\nlast", 1, "last"},
	} {
		if err := s.Append(repoID, actID, tc.text); err != nil {
			t.Fatal(err)
		}
		got, err := s.Tail(repoID, actID, tc.n)
		if err != nil || string(got) != tc.want {
			t.Fatalf("Tail(%d) = %q, %v; want %q", tc.n, got, err, tc.want)
		}
		if err := s.Remove(repoID, actID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidationMissingRemovalAndRepo(t *testing.T) {
	s := newTestStore(t)
	for _, tc := range [][2]string{{"../x", actID}, {repoID, ""}, {"not-id", actID}} {
		if _, err := s.Path(tc[0], tc[1]); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Path(%q,%q) error = %v", tc[0], tc[1], err)
		}
	}
	if _, err := s.ReadAll(repoID, actID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ReadAll = %v", err)
	}
	if err := s.Remove(repoID, actID); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(repoID, actID, "x"); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(repoID, actID); err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
	if err := s.RemoveRepo(repoID); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(repoID, actID); err != nil || exists {
		t.Fatalf("Exists after RemoveRepo = %v, %v", exists, err)
	}
}

func TestNewRejectsUnwritablePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(file, "logs")); err == nil {
		t.Fatal("New accepted a path under a regular file")
	}
}

func TestSweep(t *testing.T) {
	s := newTestStore(t)
	write := func(id, body string) string {
		t.Helper()
		if err := s.Append(repoID, id, body); err != nil {
			t.Fatal(err)
		}
		p, _ := s.Path(repoID, id)
		old := time.Now().Add(-2 * time.Minute)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	keptID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	recentID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	orphanID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	write(keptID, "record")
	recent := write(recentID, "recent")
	orphan := write(orphanID, "orphan")
	now := time.Now()
	if err := os.Chtimes(recent, now, now); err != nil {
		t.Fatal(err)
	}
	foreignDir := filepath.Join(s.dir, "foreign")
	if err := os.MkdirAll(foreignDir, 0o750); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(foreignDir, "not-a-log.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Sweep(context.Background(), func(_, id string) bool { return id == keptID })
	if err != nil || removed != 1 {
		t.Fatalf("Sweep = %d, %v", removed, err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan still exists: %v", err)
	}
	for _, p := range []string{recent, filepath.Join(s.dir, repoID, keptID+".log"), foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected retained file %q: %v", p, err)
		}
	}
	if _, err := os.Stat(foreignDir); err != nil {
		t.Fatalf("foreign directory removed: %v", err)
	}
}
