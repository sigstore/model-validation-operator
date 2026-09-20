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

type Watcher struct {
	path     string
	debounce time.Duration
	logger   logr.Logger

	mu    sync.Mutex
	timer *time.Timer
}

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

	if err := w.addRecursive(fsw, w.path); err != nil {
		fsw.Close()
		return nil, err
	}

	ch := make(chan struct{}, 1)

	go func() {
		defer fsw.Close()
		defer close(ch)

		for {
			select {
			case event, ok := <-fsw.Events:
				if !ok {
					return
				}
				if !isRelevant(event) {
					continue
				}
				w.logger.V(1).Info("File event detected", "name", event.Name, "op", event.Op.String())

				if event.Op.Has(fsnotify.Create) {
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
		select {
		case ch <- struct{}{}:
		default:
		}
	})
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
