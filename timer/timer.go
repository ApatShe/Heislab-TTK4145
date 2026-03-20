package timer

import (
	"time"
)

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
	drainTimer(t)

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

func drainTimer(t *time.Timer) {
	select {
	case <-t.C:
	default:
	}
}
