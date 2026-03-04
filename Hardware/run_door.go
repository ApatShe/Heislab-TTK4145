package door

func RunDoor(
	obstructionChan <-chan bool,
	doorTimeoutChan <-chan int,
	doorRequestChan <-chan int,
	doorCloseChan chan<- int,
	doorLampChan chan<- bool,
	resetDoorTimerChan chan<- int,
	resetObstructionWatchdogChan chan<- int,
	stopObstructionWatchdogChan chan<- int,
	obstructionInit bool,
) {
	wantOpen := false
	doorOpen := false
	obstructed := obstructionInit
	timerActive := false

	shouldOpenDoor := func() bool {
		return wantOpen
	}

	shouldKeepDoorOpen := func() bool {
		return !wantOpen && obstructed && doorOpen
	}

	shouldWaitForTimer := func() bool {
		return !wantOpen && !obstructed && doorOpen && timerActive
	}

	shouldCloseDoor := func() bool {
		return !wantOpen && !obstructed && doorOpen && !timerActive
	}

	updateDoor := func() {
		switch {
		case shouldOpenDoor():
			if !doorOpen {
				doorOpen = true
				doorLampChan <- true
			}
			resetDoorTimerChan <- 1
			timerActive = true

		case shouldKeepDoorOpen():
			resetDoorTimerChan <- 1
			timerActive = true
			resetObstructionWatchdogChan <- 1

		case shouldWaitForTimer():
			// timer still running — wait for doorTimeoutChan

		case shouldCloseDoor():
			doorOpen = false
			doorLampChan <- false
			doorCloseChan <- 1
		}
	}

	for {
		select {
		case req := <-doorRequestChan:
			wantOpen = req == 1
			updateDoor()

		case obs := <-obstructionChan:
			obstructed = obs
			if !obstructed {
				stopObstructionWatchdogChan <- 1
			}
			updateDoor()

		case <-doorTimeoutChan:
			timerActive = false
			wantOpen = false
			updateDoor()
		}
	}
}
