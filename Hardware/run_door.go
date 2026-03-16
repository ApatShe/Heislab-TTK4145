package door

// DoorIn groups all channels that deliver events into RunDoor.
type DoorIn struct {
	Obstruction          <-chan bool     // hardware obstruction sensor state
	TimerExpired         <-chan struct{} // door-open timer has fired
	OpenRequest          <-chan struct{} // elevator FSM requests the door to open
	NetworkDoorOpenState <-chan bool     // door-open state restored from network snapshot on startup
}

// DoorOut groups all channels that RunDoor writes into.
type DoorOut struct {
	Closed                   chan<- struct{} // notifies elevator FSM that door has closed
	Lamp                     chan<- bool     // drives the door-open indicator lamp
	ResetTimer               chan<- struct{} // arms/re-arms the door-open timer
	ResetObstructionWatchdog chan<- struct{} // keeps obstruction watchdog alive while obstructed
	StopObstructionWatchdog  chan<- struct{} // disarms obstruction watchdog when clear
}

func RunDoor(in DoorIn, out DoorOut, obstructionInit bool) {
	openRequested := false
	doorIsOpen := false
	isObstructed := obstructionInit
	timerIsRunning := false

	// The four predicates below encapsulate every branch of the door state
	// machine. They are named as questions so the switch reads as plain English.

	doorOpenRequestPending := func() bool {
		return openRequested
	}

	obstructionIsKeepingDoorOpen := func() bool {
		return !openRequested && isObstructed && doorIsOpen
	}

	waitingForTimerToExpire := func() bool {
		return !openRequested && !isObstructed && doorIsOpen && timerIsRunning
	}

	doorIsReadyToClose := func() bool {
		return !openRequested && !isObstructed && doorIsOpen && !timerIsRunning
	}

	updateDoor := func() {
		switch {
		case doorOpenRequestPending():
			// Consume the one-shot request immediately so subsequent obstruction
			// events fall through to obstructionIsKeepingDoorOpen() rather than
			// looping back here and re-arming the timer indefinitely.
			openRequested = false
			if !doorIsOpen {
				doorIsOpen = true
				out.Lamp <- true
			}
			out.ResetTimer <- struct{}{}
			timerIsRunning = true

		case obstructionIsKeepingDoorOpen():
			out.ResetTimer <- struct{}{}
			timerIsRunning = true
			out.ResetObstructionWatchdog <- struct{}{}

		case waitingForTimerToExpire():
			// Timer still running — nothing to do until TimerExpired fires.

		case doorIsReadyToClose():
			doorIsOpen = false
			out.Lamp <- false
			out.Closed <- struct{}{}
		}
	}

	for {
		select {
		case wasOpen := <-in.NetworkDoorOpenState:
			// One-shot: restore door-open state from the peer snapshot on startup.
			// Nil the channel so this case is never selected again.
			in.NetworkDoorOpenState = nil
			if wasOpen {
				openRequested = true
				updateDoor()
			}

		case <-in.OpenRequest:
			openRequested = true
			updateDoor()

		case obs := <-in.Obstruction:
			isObstructed = obs
			if !isObstructed {
				out.StopObstructionWatchdog <- struct{}{}
			}
			updateDoor()

		case <-in.TimerExpired:
			timerIsRunning = false
			// openRequested is cleared the moment doorOpenRequestPending() fires,
			// so this guard is a safety net only.
			openRequested = false
			updateDoor()
		}
	}
}
