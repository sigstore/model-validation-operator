// Package watcher provides filesystem event-based model change detection using inotify.
//
// Supported storage backends (local/block-backed filesystems):
//   - Local PVs, hostPath, emptyDir
//   - Block storage: NVMe, SSD, HDD, iSCSI, Ceph RBD, AWS EBS, GCE PD, Azure Disk
//
// Not supported (network filesystems where inotify cannot observe remote writes):
//   - NFS, CIFS/SMB, GlusterFS, CephFS (FUSE-mounted)
//
// For unsupported backends, the interval-based polling fallback in the agent
// ensures models are still re-validated periodically.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

const defaultDebounce = 2 * time.Second

// Watcher watches a model path (a directory tree or a single file) for
// changes and emits debounced notifications.
type Watcher struct {
	path     string
	debounce time.Duration
	logger   logr.Logger

	// target is set when path is a single file. The parent directory is
	// watched instead, so the watch survives atomic replaces (write to a temp
	// file, then rename over the target), and events are filtered to target.
	target string

	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

// New returns a Watcher for path. Call Run to start watching.
func New(path string, logger logr.Logger) *Watcher {
	return &Watcher{
		path:     path,
		debounce: defaultDebounce,
		logger:   logger.WithName("file-watcher"),
	}
}

// Run watches path for filesystem events and sends to the returned channel
// on debounced changes. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) (<-chan struct{}, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := w.addWatches(fsw); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	ch := make(chan struct{}, 1)

	go func() {
		defer func() { _ = fsw.Close() }()
		defer close(ch)
		// Runs before close(ch) so a pending debounce timer cannot send on
		// the closed channel.
		defer w.stopTimer()

		for {
			select {
			case event, ok := <-fsw.Events:
				if !ok {
					return
				}
				if !isRelevant(event) {
					continue
				}
				if w.target != "" && filepath.Clean(event.Name) != w.target {
					continue
				}
				w.logger.V(1).Info("File event detected", "name", event.Name, "op", event.Op.String())

				if w.target == "" && event.Op.Has(fsnotify.Create) {
					w.tryAddWatch(fsw, event.Name)
				}

				w.debounceSend(ch)

			case err, ok := <-fsw.Errors:
				if !ok {
					return
				}
				w.logger.Error(err, "Filesystem watcher error")

			case <-ctx.Done():
				return
			}
		}
	}()

	w.logger.Info("Watching for file changes", "path", w.path)
	return ch, nil
}

func (w *Watcher) debounceSend(ch chan<- struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		// Timer.Stop does not wait for an already running callback, so the
		// closed check under the lock is what guards against sending on ch
		// after it was closed.
		if w.closed {
			return
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	})
}

func (w *Watcher) stopTimer() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
}

func (w *Watcher) addWatches(fsw *fsnotify.Watcher) error {
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return w.addRecursive(fsw, w.path)
	}

	w.target = filepath.Clean(w.path)
	dir := filepath.Dir(w.target)
	if err := fsw.Add(dir); err != nil {
		w.logger.Error(err, "Failed to watch parent directory of model file", "path", dir)
		return err
	}
	return nil
}

func (w *Watcher) addRecursive(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if err := fsw.Add(path); err != nil {
				w.logger.Error(err, "Failed to watch directory", "path", path)
				return err
			}
		}
		return nil
	})
}

func (w *Watcher) tryAddWatch(fsw *fsnotify.Watcher, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		if err := w.addRecursive(fsw, path); err != nil {
			w.logger.Error(err, "Failed to watch new directory", "path", path)
		}
	}
}

func isRelevant(event fsnotify.Event) bool {
	return event.Op.Has(fsnotify.Create) ||
		event.Op.Has(fsnotify.Write) ||
		event.Op.Has(fsnotify.Remove) ||
		event.Op.Has(fsnotify.Rename)
}
