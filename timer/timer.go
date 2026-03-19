package timer

import (
	log "Heislab/Log"
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
	log.Log("[timer] %s started (timeout=%s panicOnTimeout=%v)", name, timeout, panicOnTimeout)
	t := time.NewTimer(timeout)
	t.Stop()
	drainTimer(t) // ensure channel is empty after Stop

	for {
		select {
		case <-stopChan:
			if !t.Stop() {
				drainTimer(t)
			}
			log.Log("[timer] %s stopped", name)

		case <-resetChan:
			if !t.Stop() {
				drainTimer(t)
			}
			t.Reset(timeout)
			log.Log("[timer] %s armed (%s)", name, timeout)

		case <-t.C:
			log.Log("[timer] %s fired", name)
			if panicOnTimeout {
				log.Log("[timer] %s exceeded limit — panicking", name)
				panic("[timer] '" + name + "' timed out")
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

// drainTimer  empties the timer channel after a Stop() that may have
// already fired. Must only be called when the timer goroutine is not running.
func drainTimer(t *time.Timer) {
	select {
	case <-t.C:
	default:
	}
}
