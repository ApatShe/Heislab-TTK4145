package elevatorcontroller

import "time"

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
