package archive

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSink records the batches it is handed instead of writing to a database.
type fakeSink struct {
	mu      sync.Mutex
	batches int
	rows    int
}

func (f *fakeSink) write(_ context.Context, batch []record) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches++
	f.rows += len(batch)
}

func (f *fakeSink) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows
}

// waitFor polls cond for up to ~2s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 200 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFlushOnBatchSize(t *testing.T) {
	fs := &fakeSink{}
	w := New("", WithBatchSize(3), WithFlushInterval(time.Hour))
	w.sink = fs
	go w.run()
	defer func() { close(w.stop); <-w.done }()

	for i := range 3 {
		w.State("sn", "aircraft", int64(i), []byte(`{}`))
	}
	waitFor(t, "batch-size flush", func() bool { return fs.rowCount() == 3 })
}

func TestFlushOnInterval(t *testing.T) {
	fs := &fakeSink{}
	w := New("", WithBatchSize(1000), WithFlushInterval(40*time.Millisecond))
	w.sink = fs
	go w.run()
	defer func() { close(w.stop); <-w.done }()

	w.State("sn", "aircraft", 1, []byte(`{}`))
	waitFor(t, "interval flush", func() bool { return fs.rowCount() == 1 })
}

func TestFlushRemainingOnStop(t *testing.T) {
	fs := &fakeSink{}
	w := New("", WithBatchSize(1000), WithFlushInterval(time.Hour))
	w.sink = fs
	go w.run()

	for i := range 5 {
		w.State("sn", "aircraft", int64(i), []byte(`{}`))
	}
	close(w.stop)
	<-w.done

	if got := fs.rowCount(); got != 5 {
		t.Fatalf("flushed %d rows on stop, want 5", got)
	}
}

func TestEnqueueDropsWhenQueueFull(t *testing.T) {
	// run() is not started, so nothing drains the queue.
	w := New("")
	w.queue = make(chan record, 4)

	for i := range 10 {
		w.State("sn", "aircraft", int64(i), nil)
	}
	if got := w.dropped.Load(); got != 6 {
		t.Fatalf("dropped = %d, want 6 (10 enqueued, queue holds 4)", got)
	}
}
