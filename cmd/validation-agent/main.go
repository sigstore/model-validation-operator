// Package main implements the validation agent that wraps model-transparency-cli
// for both one-shot and continuous validation of AI models.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
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

	flag.DurationVar(&interval, "interval", 0, "Validation interval (e.g., 5m, 1h). If 0 or not set, runs once and exits.")
	flag.IntVar(&healthPort, "health-port", 8080, "Health check server port")
	flag.Parse()

	log.SetLogger(zap.New())
	logger := log.Log.WithName("validation-agent")

	logger.Info("Starting validation agent", "interval", interval, "healthPort", healthPort)

	// Start health server
	go startHealthServer(healthPort, logger)

	// Validation args are remaining flags (passed to model-transparency-cli)
	validationArgs := flag.Args()

	// Run initial validation
	logger.Info("Running initial validation")
	if err := runValidation(validationArgs, logger); err != nil {
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

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Continuous validation loop
	if interval > 0 {
		logger.Info("Starting continuous validation loop")
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				logger.Info("Running periodic validation")
				if err := runValidation(validationArgs, logger); err != nil {
					logger.Error(err, "Validation failed")
					// Don't unmark ready - once ready, stay ready
					// This allows the pod to continue running despite transient failures
				} else {
					logger.Info("Validation successful")
					markReady() // Ensure ready state persists
				}
			case <-ctx.Done():
				logger.Info("Shutting down gracefully")
				return
			}
		}
	} else {
		// One-shot mode: wait for signal (keep container running if validation succeeded)
		logger.Info("One-shot validation complete, waiting for shutdown signal")
		<-ctx.Done()
		logger.Info("Shutting down gracefully")
	}
}

func runValidation(args []string, logger logr.Logger) error {
	cmd := exec.Command("/usr/local/bin/model_signing", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error(err, "Validation command failed", "output", string(output))
		return fmt.Errorf("validation failed: %w", err)
	}

	logger.V(1).Info("Validation output", "output", string(output))
	return nil
}

func startHealthServer(port int, logger logr.Logger) {
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

	if err := server.ListenAndServe(); err != nil {
		logger.Error(err, "Health server failed")
		os.Exit(1)
	}
}

func markReady() {
	readyMutex.Lock()
	defer readyMutex.Unlock()
	ready = true
}
