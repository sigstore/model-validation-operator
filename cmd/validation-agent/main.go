// Package main implements the validation agent that natively validates
// AI models using the model-transparency-go library for both one-shot
// and continuous validation.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/sigstore/model-validation-operator/internal/validation"
	"github.com/sigstore/model-validation-operator/internal/watcher"
	"github.com/sigstore/model-validation-operator/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	ready      bool
	readyMutex sync.RWMutex
)

func main() {
	var interval time.Duration
	var healthPort int
	var skipInitial bool
	var watch bool

	flag.DurationVar(&interval, "interval", 0, "Validation interval (e.g., 5m, 1h). If 0 or not set, runs once and exits.")
	flag.IntVar(&healthPort, "health-port", 8080, "Health check server port")
	flag.BoolVar(&skipInitial, "skip-initial", false,
		"Skip initial validation (used with legacy sidecar mode where init container already validated)")
	flag.BoolVar(&watch, "watch", false,
		"Watch model path for file changes using inotify and re-validate on change. "+
			"Works on local/block storage (NVMe, SSD, Ceph RBD, etc). "+
			"Not supported on network filesystems (NFS, CIFS) — use --interval as fallback.")
	flag.Parse()

	log.SetLogger(zap.New())
	logger := log.Log.WithName("validation-agent")

	logger.Info("Starting validation agent", "interval", interval, "healthPort", healthPort, "skipInitial", skipInitial, "watch", watch)

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	shutdownTracing, err := tracing.SetupTracing(ctx,
		tracing.WithServiceName("validation-agent"),
	)
	if err != nil {
		logger.Error(err, "Failed to initialize tracing, continuing without it")
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error(err, "Failed to shutdown tracing")
		}
	}()

	// Start health server with graceful shutdown support
	healthServerDone := make(chan struct{})
	go func() {
		startHealthServer(ctx, healthPort, logger)
		close(healthServerDone)
	}()

	// Validation args are remaining flags (passed to model-transparency-cli)
	validationArgs := flag.Args()

	// Run initial validation (unless skipped for legacy sidecar mode)
	if skipInitial {
		logger.Info("Skipping initial validation (init container already validated)")
		markReady()
	} else {
		logger.Info("Running initial validation")
		if err := runValidation(ctx, validationArgs, logger, "initial"); err != nil {
			logger.Error(err, "Initial validation failed")
			if interval == 0 {
				// One-shot mode: exit with error
				os.Exit(1)
			}
			// Continuous mode: don't mark ready, but continue (will retry)
		} else {
			logger.Info("Initial validation successful")
			markReady()
		}
	}

	// Continuous validation loop
	if interval > 0 {
		logger.Info("Starting continuous validation loop")
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var watchCh <-chan struct{}
		if watch {
			cfg, parseErr := validation.ParseArgs(validationArgs)
			if parseErr != nil {
				logger.Error(parseErr, "Failed to parse args for file watcher, continuing without watch")
			} else {
				fw := watcher.New(cfg.ModelPath, logger)
				var watchErr error
				watchCh, watchErr = fw.Run(ctx)
				if watchErr != nil {
					logger.Error(watchErr, "Failed to start file watcher, continuing with interval-only")
				}
			}
		}

		for {
			select {
			case <-ticker.C:
				logger.Info("Running periodic validation")
				if err := runValidation(ctx, validationArgs, logger, "periodic"); err != nil {
					logger.Error(err, "Validation failed")
				} else {
					logger.Info("Validation successful")
					markReady()
				}
			case _, ok := <-watchCh:
				if !ok {
					watchCh = nil
					continue
				}
				logger.Info("File change detected, running validation")
				if err := runValidation(ctx, validationArgs, logger, "file-change"); err != nil {
					logger.Error(err, "Validation failed")
				} else {
					logger.Info("Validation successful")
					markReady()
				}
			case <-ctx.Done():
				logger.Info("Shutting down gracefully")
				<-healthServerDone
				return
			}
		}
	}
	// One-shot mode: validation complete, exit immediately (for init containers)
	logger.Info("One-shot validation complete, exiting")
	cancel() // Trigger health server shutdown
	<-healthServerDone
	// Exit code 0 (success already handled, failures exit earlier at line 52)
}

func runValidation(ctx context.Context, args []string, logger logr.Logger, validationType string) error {
	tracer := otel.Tracer("validation-agent")
	ctx, span := tracer.Start(ctx, "model.validate",
		trace.WithAttributes(attribute.String("validation.type", validationType)),
	)
	defer span.End()

	cfg, err := validation.ParseArgs(args)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse validation args")
		return fmt.Errorf("failed to parse validation args: %w", err)
	}

	span.SetAttributes(
		attribute.String("validation.method", cfg.Method),
		attribute.String("validation.model_path", cfg.ModelPath),
	)

	if err := validation.Validate(ctx, cfg, logger); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "validation successful")
	return nil
}

func startHealthServer(ctx context.Context, port int, logger logr.Logger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		readyMutex.RLock()
		defer readyMutex.RUnlock()

		if ready {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		}
	})

	addr := fmt.Sprintf(":%d", port)
	logger.Info("Starting health server", "address", addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "Health server failed")
			os.Exit(1)
		}
	}()

	// Wait for context cancellation, then gracefully shutdown
	<-ctx.Done()
	logger.Info("Shutting down health server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(err, "Health server shutdown error")
	}
}

func markReady() {
	readyMutex.Lock()
	defer readyMutex.Unlock()
	ready = true
}
