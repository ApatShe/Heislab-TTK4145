package door

type DoorIn struct {
	Obstruction       <-chan bool
	TimerExpired      <-chan struct{}
	OpenRequest       <-chan struct{}
	ConfirmDoorClosed <-chan struct{}
}
type DoorOut struct {
	Closed                   chan<- struct{}
	Lamp                     chan<- bool
	ResetTimer               chan<- struct{}
	ResetObstructionWatchdog chan<- struct{}
	StopObstructionWatchdog  chan<- struct{}
}

func RunDoor(in DoorIn, out DoorOut) {
	openRequested := false
	doorIsOpen := false
	isObstructed := false
	timerIsRunning := false

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
			openRequested = false
			if !doorIsOpen {
				doorIsOpen = true
				out.Lamp <- true
				if isObstructed {
					out.ResetObstructionWatchdog <- struct{}{}
				}
			}
			out.ResetTimer <- struct{}{}
			timerIsRunning = true

		case obstructionIsKeepingDoorOpen():
			out.ResetTimer <- struct{}{}
			timerIsRunning = true

		case waitingForTimerToExpire():

		case doorIsReadyToClose():
			doorIsOpen = false
			out.Lamp <- false
			out.Closed <- struct{}{}
		}
	}

	for {
		select {

		case <-in.ConfirmDoorClosed:
			doorIsOpen = false
			out.Lamp <- false

		case <-in.OpenRequest:
			openRequested = true
			updateDoor()

		case obs := <-in.Obstruction:
			isObstructed = obs
			if !isObstructed {
				out.StopObstructionWatchdog <- struct{}{}
			} else if doorIsOpen {
				out.ResetObstructionWatchdog <- struct{}{}
			}
			updateDoor()

		case <-in.TimerExpired:
			timerIsRunning = false
			openRequested = false
			updateDoor()
		}
	}
}
