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
	initChan <-chan int,
) {
	// Block until NetworkNode signals peer state has been recovered.
	// This prevents the motor from starting before cab requests are known.
	<-initChan

	elevator := ElevatorUninitialized()
	// Start motor down if between floors; arm watchdog to detect a stall.
	if elevio.GetFloor() == -1 {
		elevio.SetMotorDirection(elevio.MD_Down)
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

		executeCommands(commands, doorRequestChan, resetMotorWatchdogTimerChan)
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
) {
	for _, cmd := range commands {
		switch cmd.Type {
		case CmdSetMotorDirection:
			dir := cmd.Value.(elevio.MotorDirection)
			elevio.SetMotorDirection(dir)
			if dir != elevio.MD_Stop {
				resetMotorWatchdogTimerChan <- 1
			}

		case CmdSetFloorIndicator:
			elevio.SetFloorIndicator(cmd.Value.(int))

		case CmdDoorRequest:
			doorRequestChan <- 1
		}
	}
}
