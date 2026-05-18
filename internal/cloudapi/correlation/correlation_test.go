package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/message"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/cloudapi/topic"
)

func TestExpectAndHandle(t *testing.T) {
	tr := New(nil)
	ch, cancel := tr.Expect("tid-1")
	defer cancel()

	err := tr.Handle(context.Background(), topic.Topic{}, &message.Envelope{Tid: "tid-1", Method: "ok"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	select {
	case got := <-ch:
		if got.Method != "ok" {
			t.Errorf("delivered envelope method = %q", got.Method)
		}
	default:
		t.Fatal("reply was not delivered to the waiting channel")
	}
}

func TestHandleUnmatchedIsNotAnError(t *testing.T) {
	// An unmatched reply (late/duplicate/foreign) must be dropped quietly.
	tr := New(nil)
	if err := tr.Handle(context.Background(), topic.Topic{}, &message.Envelope{Tid: "nobody"}); err != nil {
		t.Errorf("unmatched reply should not error, got %v", err)
	}
}

func TestAwaitResolves(t *testing.T) {
	tr := New(nil)
	got, err := tr.Await(context.Background(), "tid-y", func() error {
		// send: the reply arrives after Expect has registered.
		go func() {
			_ = tr.Handle(context.Background(), topic.Topic{}, &message.Envelope{Tid: "tid-y", Method: "done"})
		}()
		return nil
	})
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if got.Method != "done" {
		t.Errorf("got method %q, want done", got.Method)
	}
}

func TestAwaitTimeout(t *testing.T) {
	tr := New(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tr.Await(ctx, "tid-x", func() error { return nil }); err == nil {
		t.Fatal("Await should time out when no reply arrives")
	}
}

func TestCancelReleasesRegistration(t *testing.T) {
	tr := New(nil)
	_, cancel := tr.Expect("tid-c")
	cancel()
	// After cancel the tid is no longer pending, so Handle finds no match.
	if err := tr.Handle(context.Background(), topic.Topic{}, &message.Envelope{Tid: "tid-c"}); err != nil {
		t.Errorf("Handle after cancel should be a no-op, got %v", err)
	}
}
