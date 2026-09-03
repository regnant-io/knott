// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Background run execution.
//
// Runs execute on their own goroutines: an API call, a schedule, a webhook and a
// sub_workflow node all hand a run id to processRun and return. Two things were
// missing from that.
//
// Nothing knew when those goroutines finished, which made a clean shutdown
// impossible — the process could exit mid-node, leaving a run marked RUNNING
// with a lease another replica had to time out before picking up.
//
// And nothing bounded them. A burst of webhooks — a partner's retry storm, a
// backfill, a schedule firing across a thousand rows — started a goroutine per
// run, each holding an HTTP client and a SQLite connection. The failure mode is
// not a graceful slowdown: it is memory exhaustion and a database that starts
// returning "too many open files". A semaphore turns that burst into a queue,
// which is what an operator expects.
var backgroundRuns sync.WaitGroup

// runSlots bounds how many runs execute at once. Runs waiting for a slot are
// already durable in the database, so queueing costs nothing but latency.
var runSlots chan struct{}

// queuedRuns counts runs waiting for a slot, so the queue depth is visible in
// /metrics rather than only as unexplained latency.
var queuedRuns int64

func init() {
	runSlots = make(chan struct{}, resolveMaxConcurrentRuns())
}

// resolveMaxConcurrentRuns reads MAX_CONCURRENT_RUNS, defaulting to a multiple
// of the core count. Runs are overwhelmingly I/O-bound — they wait on APIs, on
// models, on people — so the useful concurrency is far above the core count; the
// limit exists to bound memory, not to keep CPUs busy.
func resolveMaxConcurrentRuns() int {
	if v := os.Getenv("MAX_CONCURRENT_RUNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("[Engine] MAX_CONCURRENT_RUNS=%q is not a positive number — using the default", v)
	}
	n := runtime.NumCPU() * 16
	if n < 32 {
		n = 32
	}
	if n > 512 {
		n = 512
	}
	return n
}

// MaxConcurrentRuns reports the configured ceiling.
func MaxConcurrentRuns() int { return cap(runSlots) }

// QueuedRuns reports how many runs are waiting for a slot.
func QueuedRuns() int { return int(atomic.LoadInt64(&queuedRuns)) }

// startRunInBackground executes a run on its own goroutine, tracked so callers
// can wait for in-flight work to settle, and bounded so a burst queues rather
// than exhausting the process.
func startRunInBackground(runID string) {
	backgroundRuns.Add(1)
	atomic.AddInt64(&queuedRuns, 1)
	go func() {
		defer backgroundRuns.Done()
		runSlots <- struct{}{}
		atomic.AddInt64(&queuedRuns, -1)
		defer func() { <-runSlots }()
		processRun(runID)
	}()
}

// WaitForBackgroundRuns blocks until every in-flight run finishes or the timeout
// passes, reporting whether everything settled. Runs are checkpointed, so a run
// abandoned at the timeout resumes on the next start rather than being lost.
func WaitForBackgroundRuns(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		backgroundRuns.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
