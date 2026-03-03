package door // delete comments before hand-in

/*
ToDo in order to make code work in general, and with rest of program
1. Who should own the open/close door channel?
2. Open/close door channel has to be merged with naming in elevator.go (CmdDoorRequest, or smth)
3. Thoughts about buffers for channels?
4. Implement timer logic
5. Should open/close door channel be pulse or boolean? I think boolean is better. -Amund
6. Is it good to have both wantOpen and obstructed defined as false when the routine starts? Could there be issues?
7. Right now this code expects to read the door state from the lights module that will be created. I.e. the lights
module has to be correct in order for this to work.
8. Currently the module assumes we have some doorState defined in WV (or smth) that is a ground truth for when the
module reboots after a crash.
9. Thoughts on telling lights to turn on a light that already should be on? Maybe more redundancy, but more traffic in program
*/

import (
	"time"
)

type DoorChannels struct {
	DoorRequestChan     chan<- bool
	DoorObstructionChan chan<- bool
}

func RunDoor() DoorChannels {
	requestCh := make(chan bool, 1)
	obstructionCh := make(chan bool, 1)

	go func() {
		wantOpen := false
		doorState := false // true = open, false = closed. This must be changed to pull correct state from VW
		obstructed := false
		timerActive := false // Don't think you need any ground truth since you essentially read this from the HW switch
		const openDoorDuration = 3 * time.Second

		timer := time.NewTimer(time.Hour)
		timer.Stop()

		var timerCh <-chan time.Time = nil

		resetAndStartTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(openDoorDuration)
			timerCh = timer.C
			timerActive = true
		}

		for {
			select {
			case wantOpen = <-requestCh:
			case obstructed = <-obstructionCh:
			case <-timerCh: // timer expired, door is allowed to close
				timerActive = false
				timerCh = nil
			}

			switch { // open door -> tell lights to turn on doorLight, and vice-versa
			case wantOpen && obstructed:
				// open door (obstruction is irrelevant when opening door)
				doorState = true
				// tell lights to turn on doorLight
				resetAndStartTimer()

			case wantOpen && !obstructed:
				// open door (closed door with no obstruction - all good)
				doorState = true
				// tell lights to turn on doorLight
				resetAndStartTimer()

			case !wantOpen && obstructed && doorState:
				// open door (keep door open since obstruction is active, even when you want to close)
				// tell lights to turn on doorLight (should already be on, but no harm in doing it twice?, check ToDo #9)
				resetAndStartTimer()

			case !wantOpen && obstructed && !doorState:
				// close door (door is already closed, so nothing to do here)

			case !wantOpen && !obstructed && doorState && timerActive:
				// open (want to close, no obstruction, door is open, can't close since 3s timer is active)
				// tell lights to turn on doorLight (if we want to spam, check ToDo #9)

			case !wantOpen && !obstructed && doorState && !timerActive:
				// close (want to close, no obstruction, door is open, can close since 3s timer is inactive)
				// tell lights to turn off doorLight
				doorState = false
			}

		}
	}()

	return DoorChannels{
		DoorRequestChan:     requestCh,
		DoorObstructionChan: obstructionCh,
	}
}
