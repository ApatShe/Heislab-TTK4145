package door

import (
	log "Heislab/Log"
)

// DoorIn groups all channels that deliver events into RunDoor.
type DoorIn struct {
	Obstruction       <-chan bool     // hardware obstruction sensor state
	TimerExpired      <-chan struct{} // door-open timer has fired
	OpenRequest       <-chan struct{} // elevator FSM requests the door to open
	ConfirmDoorClosed <-chan struct{} // signal from elevator FSM that door is closed on startup
}

// DoorOut groups all channels that RunDoor writes into.
type DoorOut struct {
	Closed                   chan<- struct{} // notifies elevator FSM that door has closed
	Lamp                     chan<- bool     // drives the door-open indicator lamp
	ResetTimer               chan<- struct{} // arms/re-arms the door-open timer
	ResetObstructionWatchdog chan<- struct{} // arms obstruction watchdog when door is blocked
	StopObstructionWatchdog  chan<- struct{} // disarms obstruction watchdog when clear
}

func RunDoor(in DoorIn, out DoorOut) {
	openRequested := false
	doorIsOpen := false
	isObstructed := false
	timerIsRunning := false

	log.Log("[door] starting: waiting for door state confirmation from elevator")

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
				log.Log("[door] opening door, lamp on")
				if isObstructed {
					// Obstruction was already active when door opened — arm watchdog now.
					out.ResetObstructionWatchdog <- struct{}{}
					log.Log("[door] obstruction watchdog armed (obstruction was already active)")
				}
			}
			out.ResetTimer <- struct{}{}
			timerIsRunning = true
			log.Log("[door] door timer armed")

		case obstructionIsKeepingDoorOpen():
			// Re-arm door timer only — obstruction watchdog runs uninterrupted.
			out.ResetTimer <- struct{}{}
			timerIsRunning = true
			log.Log("[door] obstructed — door timer re-armed")

		case waitingForTimerToExpire():
			log.Log("[door] waiting for door timer")

		case doorIsReadyToClose():
			doorIsOpen = false
			out.Lamp <- false
			out.Closed <- struct{}{}
			log.Log("[door] closing door, lamp off, notifying FSM")
		}
	}

	for {
		select {

		case <-in.ConfirmDoorClosed:
			doorIsOpen = false
			out.Lamp <- false
			log.Log("[door] door closed confirmed at startup, lamp off")

		case <-in.OpenRequest:
			log.Log("[door] open request received")
			openRequested = true
			updateDoor()

		case obs := <-in.Obstruction:
			isObstructed = obs
			log.Log("[door] obstruction sensor state changed: %v", obs)
			if !isObstructed {
				out.StopObstructionWatchdog <- struct{}{}
				log.Log("[door] obstruction cleared, watchdog stopped")
			} else if doorIsOpen {
				// Only arm if door is actually open — obstruction during travel is ignored.
				out.ResetObstructionWatchdog <- struct{}{}
				log.Log("[door] obstruction watchdog armed")
			}
			updateDoor()

		case <-in.TimerExpired:
			timerIsRunning = false
			// openRequested is cleared the moment doorOpenRequestPending() fires,
			// so this guard is a safety net only.
			openRequested = false
			log.Log("[door] door timer expired")
			updateDoor()
		}
	}
}
