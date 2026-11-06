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
	var maxConsecutiveFailures int

	flag.DurationVar(&interval, "interval", 0, "Validation interval (e.g., 5m, 1h). If 0 or not set, runs once and exits.")
	flag.IntVar(&healthPort, "health-port", 8080, "Health check server port")
	flag.BoolVar(&skipInitial, "skip-initial", false,
		"Skip initial validation (used with legacy sidecar mode where init container already validated)")
	flag.IntVar(&maxConsecutiveFailures, "max-consecutive-failures", 0,
		"Number of consecutive periodic validation failures after which readiness is revoked. "+
			"0 means never revoke readiness once ready (fail-open).")
	flag.Parse()

	log.SetLogger(zap.New())
	logger := log.Log.WithName("validation-agent")

	logger.Info("Starting validation agent",
		"interval", interval,
		"healthPort", healthPort,
		"skipInitial", skipInitial,
		"maxConsecutiveFailures", maxConsecutiveFailures)

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
		logger.Info("Starting continuous validation loop", "maxConsecutiveFailures", maxConsecutiveFailures)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		continuousValidationLoop(ctx, ticker.C, maxConsecutiveFailures, func() error {
			return runValidation(ctx, validationArgs, logger, "periodic")
		}, logger)

		logger.Info("Shutting down gracefully")
		<-healthServerDone
		return
	}
	// One-shot mode: validation complete, exit immediately (for init containers)
	logger.Info("One-shot validation complete, exiting")
	cancel() // Trigger health server shutdown
	<-healthServerDone
	// Exit code 0 (success already handled, failures exit earlier at line 52)
}

// continuousValidationLoop runs validate on every tick until ctx is cancelled.
// maxConsecutiveFailures controls when readiness is revoked after repeated
// failures; see handleValidationResult for the exact semantics.
func continuousValidationLoop(
	ctx context.Context, tick <-chan time.Time, maxConsecutiveFailures int,
	validate func() error, logger logr.Logger,
) {
	consecutiveFailures := 0
	for {
		select {
		case <-tick:
			logger.Info("Running periodic validation")
			err := validate()
			consecutiveFailures = handleValidationResult(err, consecutiveFailures, maxConsecutiveFailures)
			if err != nil {
				logger.Error(err, "Validation failed",
					"consecutiveFailures", consecutiveFailures,
					"maxConsecutiveFailures", maxConsecutiveFailures,
					"ready", isReady())
			} else {
				logger.Info("Validation successful")
			}
		case <-ctx.Done():
			return
		}
	}
}

// handleValidationResult updates readiness based on a validation outcome and
// returns the new consecutive-failure count.
//
// On success the counter resets to 0 and the agent is marked ready. On failure
// the counter increments; when maxConsecutiveFailures > 0 and the counter
// reaches it, readiness is revoked (fail-closed). A maxConsecutiveFailures of 0
// never revokes readiness (fail-open), preserving the legacy behavior.
func handleValidationResult(err error, consecutiveFailures, maxConsecutiveFailures int) int {
	if err == nil {
		markReady()
		return 0
	}

	consecutiveFailures++
	if maxConsecutiveFailures > 0 && consecutiveFailures >= maxConsecutiveFailures {
		markNotReady()
	}
	return consecutiveFailures
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
		if isReady() {
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

func markNotReady() {
	readyMutex.Lock()
	defer readyMutex.Unlock()
	ready = false
}

func isReady() bool {
	readyMutex.RLock()
	defer readyMutex.RUnlock()
	return ready
}
