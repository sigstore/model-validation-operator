package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestWatcher_FileWrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(f, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	logger := zap.New()
	w := New(dir, logger)
	w.debounce = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(f, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file write event")
	}
}

func TestWatcher_FileCreate(t *testing.T) {
	dir := t.TempDir()

	logger := zap.New()
	w := New(dir, logger)
	w.debounce = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "new-model.bin"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file create event")
	}
}

func TestWatcher_FileRemove(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	logger := zap.New()
	w := New(dir, logger)
	w.debounce = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file remove event")
	}
}

func TestWatcher_SubdirectoryCreate(t *testing.T) {
	dir := t.TempDir()

	logger := zap.New()
	w := New(dir, logger)
	w.debounce = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "submodel")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	// Wait for subdirectory watch to be established, then write a file in it
	time.Sleep(200 * time.Millisecond)

	// Drain any event from the mkdir itself
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
	}

	if err := os.WriteFile(filepath.Join(subdir, "weights.bin"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event in new subdirectory")
	}
}

func TestWatcher_Debounce(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(f, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	logger := zap.New()
	w := New(dir, logger)
	w.debounce = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Rapid-fire writes should produce a single debounced event
	for i := range 10 {
		if err := os.WriteFile(f, []byte{byte(i)}, 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for debounced event")
	}

	// No second event should arrive (channel buffer is 1, debounce coalesces)
	select {
	case <-ch:
		t.Fatal("unexpected second event after debounce")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	dir := t.TempDir()

	logger := zap.New()
	w := New(dir, logger)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	// Channel should be closed after context cancellation
	select {
	case _, ok := <-ch:
		if ok {
			// Might get a buffered event, drain it
			select {
			case _, ok := <-ch:
				if ok {
					t.Fatal("channel should be closed after context cancellation")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for channel close")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel close after cancel")
	}
}
