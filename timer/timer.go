package timer

import (
	"fmt"
	"time"
)

/*
// DoorTimer wraps a time.Timer to provide start/stop/channel
// semantics matching the C elevator algorithm's timer module.
type DoorTimer struct {
	timer *time.Timer
}

// NewDoorTimer creates a new stopped door timer.
func NewDoorTimer() *DoorTimer {
	t := time.NewTimer(time.Hour)
	t.Stop()
	return &DoorTimer{timer: t}
}

// Start (re)starts the timer with the given duration.
// If the timer is already running, it is reset.
func (dt *DoorTimer) Start(d time.Duration) {
	dt.timer.Reset(d)
}

// Stop cancels a pending timer.
func (dt *DoorTimer) Stop() {
	dt.timer.Stop()
}

// Chan returns the channel that fires when the timer expires.
// Use this in a select statement in the controller event loop.
func (dt *DoorTimer) Chan() <-chan time.Time {
	return dt.timer.C
}
*/

type timer struct {
	startTime     time.Time
	active        bool
	timedOutCache bool
	timeout       time.Duration
}

func RunTimer(
	resetChan <-chan int,
	stopChan <-chan int,
	timeoutChan chan<- int,

	timeout time.Duration,
	panicOnTimeout bool,
	name string,
) {
	timerInstance := newTimer(timeout)

	for {
		select {
		case <-stopChan:
			timerInstance.active = false
		case <-resetChan:
			timerInstance.startTime = time.Now()
			timerInstance.active = true
		default:
			timedOut := CheckTimeout(timerInstance)
			if timedOut {
				timerInstance.active = false
				if panicOnTimeout {
					panic(fmt.Sprintf("Panicking timer %s timed out", name))
				}
				timeoutChan <- 1
			}
			timerInstance.timedOutCache = timedOut
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func CheckTimeout(_timer timer) bool {
	timedOut := _timer.active && time.Since(_timer.startTime) > _timer.timeout
	return timedOut && timedOut != _timer.timedOutCache
}

func newTimer(timeout time.Duration) timer {
	return timer{
		startTime:     time.Now(),
		active:        false,
		timedOutCache: false,
		timeout:       timeout,
	}
}
