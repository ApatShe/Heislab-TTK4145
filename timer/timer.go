package timer

import (
	"fmt"
	"time"
)

type timer struct {
	startTime time.Time
	active    bool
	timeout   time.Duration
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
			if CheckTimeout(timerInstance) {
				timerInstance.active = false
				if panicOnTimeout {
					panic(fmt.Sprintf("Panicking timer %s timed out", name))
				}
				if timeoutChan != nil {
					timeoutChan <- 1
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func CheckTimeout(_timer timer) bool {
	return _timer.active && time.Since(_timer.startTime) > _timer.timeout
}

func newTimer(timeout time.Duration) timer {
	return timer{
		startTime: time.Now(),
		active:    false,
		timeout:   timeout,
	}
}
