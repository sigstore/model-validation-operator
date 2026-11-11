// Copyright 2025 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetReady puts the shared readiness state into a known value for a test.
// The agent uses a package-level ready flag, so tests must set it explicitly.
func resetReady(t *testing.T, value bool) {
	t.Helper()
	if value {
		markReady()
	} else {
		markNotReady()
	}
}

func TestHandleValidationResult_SuccessMarksReadyAndResets(t *testing.T) {
	resetReady(t, false)

	// A success after prior failures resets the counter and marks ready.
	count := handleValidationResult(nil, 5, 3)

	assert.Equal(t, 0, count, "success must reset the consecutive-failure count")
	assert.True(t, isReady(), "success must mark the agent ready")
}

func TestHandleValidationResult_FailOpenNeverRevokes(t *testing.T) {
	resetReady(t, true)

	// maxConsecutiveFailures == 0 means fail-open: readiness is never revoked.
	count := 0
	for i := 0; i < 10; i++ {
		count = handleValidationResult(errors.New("boom"), count, 0)
		require.True(t, isReady(), "fail-open must keep the agent ready after failure %d", i+1)
	}
	assert.Equal(t, 10, count, "the counter still increments while fail-open")
}

func TestHandleValidationResult_ThresholdOneRevokesImmediately(t *testing.T) {
	resetReady(t, true)

	count := handleValidationResult(errors.New("boom"), 0, 1)

	assert.Equal(t, 1, count)
	assert.False(t, isReady(), "threshold of 1 must revoke readiness on the first failure")
}

func TestHandleValidationResult_RevokesOnlyAtThreshold(t *testing.T) {
	resetReady(t, true)

	count := 0

	count = handleValidationResult(errors.New("boom"), count, 3)
	assert.Equal(t, 1, count)
	assert.True(t, isReady(), "must stay ready before reaching the threshold")

	count = handleValidationResult(errors.New("boom"), count, 3)
	assert.Equal(t, 2, count)
	assert.True(t, isReady(), "must stay ready before reaching the threshold")

	count = handleValidationResult(errors.New("boom"), count, 3)
	assert.Equal(t, 3, count)
	assert.False(t, isReady(), "must revoke readiness once the threshold is reached")
}

func TestHandleValidationResult_SuccessResetsBeforeThreshold(t *testing.T) {
	resetReady(t, true)

	count := 0
	// Two failures, then a success: the counter resets so a later streak starts over.
	count = handleValidationResult(errors.New("boom"), count, 3)
	count = handleValidationResult(errors.New("boom"), count, 3)
	require.Equal(t, 2, count)
	require.True(t, isReady())

	count = handleValidationResult(nil, count, 3)
	require.Equal(t, 0, count)
	require.True(t, isReady())

	// Two more failures must not revoke readiness: the streak is only 2, not 3.
	count = handleValidationResult(errors.New("boom"), count, 3)
	count = handleValidationResult(errors.New("boom"), count, 3)
	assert.Equal(t, 2, count)
	assert.True(t, isReady(), "a success must prevent non-consecutive failures from revoking readiness")
}

func TestHandleValidationResult_RecoversAfterRevoke(t *testing.T) {
	resetReady(t, true)

	// Revoke via threshold of 1, then recover on the next success.
	count := handleValidationResult(errors.New("boom"), 0, 1)
	require.False(t, isReady())

	count = handleValidationResult(nil, count, 1)
	assert.Equal(t, 0, count)
	assert.True(t, isReady(), "a successful validation must restore readiness")
}

func TestContinuousValidationLoop_RevokesReadinessOnTick(t *testing.T) {
	resetReady(t, true)

	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		continuousValidationLoop(ctx, tick, 1, func() error {
			return errors.New("boom")
		}, logr.Discard())
		close(done)
	}()

	tick <- time.Now()

	require.Eventually(t, func() bool {
		return !isReady()
	}, time.Second, 5*time.Millisecond, "loop must revoke readiness after a failing tick")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not return after context cancellation")
	}
}

func TestContinuousValidationLoop_StopsOnContextCancel(t *testing.T) {
	resetReady(t, true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		continuousValidationLoop(ctx, make(chan time.Time), 1, func() error {
			t.Error("validate must not be called when no tick fires")
			return nil
		}, logr.Discard())
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not return after context cancellation")
	}
}
