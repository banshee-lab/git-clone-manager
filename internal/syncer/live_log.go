package syncer

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"gitsync/internal/logstore"
)

const liveReadSize = 64 << 10

type Chunk struct {
	Data   string `json:"chunk"`
	Offset int64  `json:"offset"`
}

// LiveLog synchronously persists output and wakes readers to tail the file.
type LiveLog struct {
	repoID, activityID string
	logs               logstore.Store
	writer             *logstore.Writer
	size               atomic.Int64
	mu                 sync.Mutex
	closed             bool
	wake               chan struct{}
}

func NewLiveLog(repoID, activityID string, logs logstore.Store, writer *logstore.Writer) (*LiveLog, error) {
	if logs == nil || writer == nil {
		return nil, errors.New("syncer: live log requires a log store and writer")
	}
	n, err := writer.Size()
	if err != nil {
		return nil, err
	}
	l := &LiveLog{repoID: repoID, activityID: activityID, logs: logs, writer: writer, wake: make(chan struct{}, 1)}
	l.size.Store(n)
	return l, nil
}

func (l *LiveLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := l.writer.Write(p)
	l.size.Add(int64(n))
	select {
	case l.wake <- struct{}{}:
	default:
	}
	return n, err
}

func (l *LiveLog) Size() int64 { return l.size.Load() }

func (l *LiveLog) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	err := l.writer.Close()
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
	return err
}

// Snapshot returns valid UTF-8 output through the current file size.
func (l *LiveLog) Snapshot() (string, int64, error) {
	data, offset, err := l.logs.ReadRange(l.repoID, l.activityID, 0, l.size.Load())
	return string(data), offset, err
}

// Subscribe tails persisted bytes from offset; each reader owns its goroutine.
func (l *LiveLog) Subscribe(ctx context.Context, fromOffset int64) <-chan Chunk {
	out := make(chan Chunk, 1)
	if ctx == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		offset := fromOffset
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			for {
				data, next, err := l.logs.ReadRange(l.repoID, l.activityID, offset, liveReadSize)
				if err == nil && len(data) > 0 {
					offset = next
					select {
					case out <- Chunk{Data: string(data), Offset: next}:
					case <-ctx.Done():
						return
					}
					continue
				}
				break
			}
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed && offset >= l.size.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-l.wake:
			case <-ticker.C:
			}
		}
	}()
	return out
}
