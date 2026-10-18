// Package logstore stores activity output in append-only files.
package logstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"context"
	"time"
)

var (
	ErrInvalidID = errors.New("logstore: invalid UUID")
	ErrNotFound  = errors.New("logstore: not found")
	idPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Store is the log file API consumed by history cleanup and the sync executor.
type Store interface {
	Create(repoID, actID string) (*Writer, error)
	Append(repoID, actID, text string) error
	ReadAll(repoID, actID string) ([]byte, error)
	ReadRange(repoID, actID string, offset, max int64) ([]byte, int64, error)
	Size(repoID, actID string) (int64, error)
	Tail(repoID, actID string, nLines int) ([]byte, error)
	Exists(repoID, actID string) (bool, error)
	Remove(repoID, actID string) error
	RemoveRepo(repoID string) error
	Sweep(context.Context, func(repoID, actID string) bool) (int, error)
}

type FS struct{ dir string }

// New creates and verifies a writable log directory.
func New(dir string) (*FS, error) {
	if dir == "" {
		return nil, errors.New("logstore: directory is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("logstore: create directory %q: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".gitsync-write-check-*")
	if err != nil {
		return nil, fmt.Errorf("logstore: directory %q is not writable: %w", dir, err)
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return nil, fmt.Errorf("logstore: verify directory %q: close check file: %w", dir, closeErr)
	}
	if removeErr != nil {
		return nil, fmt.Errorf("logstore: verify directory %q: remove check file: %w", dir, removeErr)
	}
	return &FS{dir: dir}, nil
}

func validID(id string) bool { return idPattern.MatchString(id) }

func (s *FS) Path(repoID, actID string) (string, error) {
	if !validID(repoID) || !validID(actID) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.dir, repoID, actID+".log"), nil
}

type Writer struct {
	mu   sync.Mutex
	file *os.File
}

func (s *FS) Create(repoID, actID string) (*Writer, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("logstore: create repository directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("logstore: create activity log: %w", err)
	}
	return &Writer{file: f}, nil
}
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	return w.file.Write(p)
}
func (w *Writer) Size() (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	st, err := w.file.Stat()
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
func (s *FS) Append(repoID, actID, text string) error {
	w, err := s.Create(repoID, actID)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(w, text)
	closeErr := w.Close()
	if writeErr != nil {
		return fmt.Errorf("logstore: append: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("logstore: close appended log: %w", closeErr)
	}
	return nil
}
func openRead(path string) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}
func (s *FS) ReadAll(repoID, actID string) ([]byte, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return nil, err
	}
	f, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("logstore: read activity log: %w", err)
	}
	return b, nil
}
func (s *FS) ReadRange(repoID, actID string, offset, max int64) ([]byte, int64, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return nil, offset, err
	}
	if offset < 0 || max < 0 {
		return nil, offset, errors.New("logstore: negative range")
	}
	f, err := openRead(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset > st.Size() {
		offset = st.Size()
	}
	// If the requested offset lands in a UTF-8 continuation byte, move to the
	// next rune boundary. This keeps each returned range independently valid.
	start := offset
	if start > 0 && start < st.Size() {
		var one [1]byte
		for start < st.Size() {
			if _, err := f.ReadAt(one[:], start); err != nil {
				return nil, start, err
			}
			if one[0]&0xc0 != 0x80 {
				break
			}
			start++
		}
	}
	if max == 0 || start == st.Size() {
		return []byte{}, start, nil
	}
	n := max
	if remain := st.Size() - start; n > remain {
		n = remain
	}
	b := make([]byte, n)
	got, readErr := f.ReadAt(b, start)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, start, readErr
	}
	b = b[:got]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b, start + int64(len(b)), nil
}
func (s *FS) Size(repoID, actID string) (int64, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return 0, err
	}
	f, err := openRead(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
func (s *FS) Tail(repoID, actID string, nLines int) ([]byte, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return nil, err
	}
	f, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if nLines <= 0 || st.Size() == 0 {
		return []byte{}, nil
	}
	const chunkSize int64 = 4096
	var pos int64 = st.Size()
	lines := 0
	var data []byte
	for pos > 0 {
		n := chunkSize
		if n > pos {
			n = pos
		}
		pos -= n
		part := make([]byte, n)
		if _, err := f.ReadAt(part, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		data = append(part, data...)
		for i := len(part) - 1; i >= 0; i-- {
			if part[i] == '\n' {
				if pos+int64(i) == st.Size()-1 { // a trailing newline is not an extra line
					continue
				}
				lines++
				if lines >= nLines {
					data = data[len(data)-len(part)+i+1:]
					return data, nil
				}
			}
		}
	}
	// A final newline terminates the last line; do not count an empty line after it.
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return []byte(strings.TrimSuffix(string(data), "\n")), nil
	}
	return data, nil
}
func (s *FS) Exists(repoID, actID string) (bool, error) {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (s *FS) Remove(repoID, actID string) error {
	path, err := s.Path(repoID, actID)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logstore: remove activity log: %w", err)
	}
	return nil
}
func (s *FS) RemoveRepo(repoID string) error {
	if !validID(repoID) {
		return ErrInvalidID
	}
	err := os.RemoveAll(filepath.Join(s.dir, repoID))
	if err != nil {
		return fmt.Errorf("logstore: remove repository logs: %w", err)
	}
	return nil
}
func (s *FS) Sweep(ctx context.Context, hasRecord func(repoID, actID string) bool) (int, error) {
	if ctx == nil {
		return 0, errors.New("logstore: nil context")
	}
	if hasRecord == nil {
		return 0, errors.New("logstore: nil record lookup")
	}
	removed := 0
	err := filepath.WalkDir(s.dir, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 || !validID(parts[0]) || !strings.HasSuffix(parts[1], ".log") || !validID(strings.TrimSuffix(parts[1], ".log")) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || time.Since(info.ModTime()) < 60*time.Second {
			return nil
		}
		if hasRecord(parts[0], strings.TrimSuffix(parts[1], ".log")) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("logstore: sweep: %w", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return removed, fmt.Errorf("logstore: inspect repository directories: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		dirPath := filepath.Join(s.dir, entry.Name())
		children, err := os.ReadDir(dirPath)
		if err != nil {
			return removed, fmt.Errorf("logstore: inspect repository directory: %w", err)
		}
		if len(children) != 0 {
			continue
		}
		if err := os.Remove(dirPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("logstore: remove empty repository directory: %w", err)
		}
	}
	return removed, nil
}
