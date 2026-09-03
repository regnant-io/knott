package execution

import (
	"sync"
	"time"
)

// Background run tracking.
//
// Runs execute on their own goroutines: an API call, a schedule, a webhook and a
// sub_workflow node all hand a run id to processRun and return. Nothing used to
// know when those goroutines finished, which made a clean shutdown impossible —
// the process could exit mid-node, leaving a run marked RUNNING with a lease
// that another replica had to time out before picking up.
var backgroundRuns sync.WaitGroup

// startRunInBackground executes a run on its own goroutine, tracked so callers
// can wait for in-flight work to settle.
func startRunInBackground(runID string) {
	backgroundRuns.Add(1)
	go func() {
		defer backgroundRuns.Done()
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
