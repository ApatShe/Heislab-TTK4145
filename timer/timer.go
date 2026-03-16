package timer

import (
	"fmt"
	"time"
)

// RunTimer is a general-purpose timer goroutine.
//
// It uses time.NewTimer internally — no busy-polling. A nil stopChan or
// timeoutChan is valid: nil channels block forever in a select and are
// therefore safely ignored.
//
// panicOnTimeout is kept for the obstruction watchdog (a door that will not
// close is a hard failure). For the motor watchdog, pass panicOnTimeout=false
// and a real timeoutChan so the stall can be handled gracefully.
func RunTimer(
	resetChan <-chan struct{},
	stopChan <-chan struct{},
	timeoutChan chan<- struct{},
	timeout time.Duration,
	panicOnTimeout bool,
	name string,
) {
	t := time.NewTimer(timeout)
	t.Stop()
	drainTimer(t) // ensure channel is empty after Stop

	for {
		select {
		case <-stopChan:
			if !t.Stop() {
				drainTimer(t)
			}

		case <-resetChan:
			if !t.Stop() {
				drainTimer(t)
			}
			t.Reset(timeout)

		case <-t.C:
			if panicOnTimeout {
				panic(fmt.Sprintf("Timer %q timed out", name))
			}
			if timeoutChan != nil {
				select {
				case timeoutChan <- struct{}{}:
				default:
				}
			}
		}
	}
}

// drainTimer safely empties the timer channel after a Stop() that may have
// already fired. Must only be called when the timer goroutine is not running.
func drainTimer(t *time.Timer) {
	select {
	case <-t.C:
	default:
	}
}
