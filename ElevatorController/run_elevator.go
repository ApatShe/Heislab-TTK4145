package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

// RunElevator runs the elevator finite state machine.
//
// Two distinct order paths:
//   - cabOrderChan  — cab button events arriving directly from hardware polling.
//   - hallRequestChan — full hall-request matrix pushed by the manager after the
//     HRA has run and the network has reached consensus.
//
// Hall button presses are never sent here directly; they travel:
//
//	button → RunNetworkNode → manager/HRA → network broadcast → consensus
//	→ manager pushes [][2]bool matrix → hallRequestChan → here.
func RunElevator(
	floorChan <-chan int,
	cabOrderChan <-chan elevio.ButtonEvent,
	hallRequestChan <-chan [][2]bool,
	doorCloseChan <-chan int,
	doorRequestChan chan<- int,
	lightsElevatorStateChan chan<- Elevator,
	elevatorStateChan chan<- Elevator,
	resetMotorWatchdogTimerChan chan<- int,
	stopMotorWatchdogTimerChan chan<- int,
) {
	elevator := ElevatorUninitialized()

	// If we start between floors the motor was already started by InitBetweenFloors;
	// arm the motor watchdog so a stall is detected.
	if elevio.GetFloor() == -1 {
		elevator.Direction = elevio.MD_Down
		elevator.Behaviour = EB_Moving
		resetMotorWatchdogTimerChan <- 1
	}

	broadcast := func() {
		select {
		case elevatorStateChan <- *elevator:
		default:
		}
		select {
		case lightsElevatorStateChan <- *elevator:
		default:
		}
	}

	fmt.Println("Elevator controller started!")

	for {
		var commands []ElevatorCommand

		select {
		case btn := <-cabOrderChan:
			// Cab requests are local — handle immediately.
			fmt.Printf("Cab button: Floor=%d\n", btn.Floor)
			_, commands = FsmOnCabRequest(elevator, btn.Floor)

		case newHallRequests := <-hallRequestChan:
			// Hall-request matrix assigned by manager after HRA + network consensus.
			fmt.Printf("Hall request update from manager\n")
			_, commands = FsmOnHallRequestsUpdate(elevator, newHallRequests)

		case floor := <-floorChan:
			_, commands = FsmOnFloorArrival(elevator, floor)

		case <-doorCloseChan:
			_, commands = FsmOnDoorClose(elevator)

		}

		executeCommands(commands, doorRequestChan, resetMotorWatchdogTimerChan, stopMotorWatchdogTimerChan)
		broadcast()
	}
}

// executeCommands sends the hardware actions produced by the FSM to their
// respective drivers. Motor-timer resets/stops are side effects of
// CmdSetMotorDirection and CmdSetFloorIndicator — not separate command types.
func executeCommands(
	commands []ElevatorCommand,
	doorRequestChan chan<- int,
	resetMotorWatchdogTimerChan chan<- int,
	stopMotorWatchdogTimerChan chan<- int,
) {
	for _, cmd := range commands {
		switch cmd.Type {
		case CmdSetMotorDirection:
			dir := cmd.Value.(elevio.MotorDirection)
			elevio.SetMotorDirection(dir)
			if dir != elevio.MD_Stop {
				resetMotorWatchdogTimerChan <- 1
			} else {
				stopMotorWatchdogTimerChan <- 1
			}

		case CmdSetFloorIndicator:
			elevio.SetFloorIndicator(cmd.Value.(int))
			stopMotorWatchdogTimerChan <- 1

		case CmdDoorRequest:
			doorRequestChan <- 1
		}
	}
}
