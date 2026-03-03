package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

// FsmActOnBehaviourPair writes direction+behaviour into elevator and returns
// the hardware commands to execute alongside any served hall requests.
// Only caller of RequestsClearAtCurrentFloor.
func FsmActOnBehaviourPair(elevator *Elevator, pair DirnBehaviourPair) ([]elevio.ButtonEvent, []ElevatorCommand) {
	elevator.Direction = pair.Direction
	elevator.Behaviour = pair.Behaviour

	switch pair.Behaviour {
	case EB_DoorOpen:
		// Notify the door module to open the door and start its timer.
		return RequestsClearAtCurrentFloor(elevator), []ElevatorCommand{
			{Type: CmdDoorRequest},
		}

	case EB_Moving:
		return nil, []ElevatorCommand{
			{Type: CmdSetMotorDirection, Value: elevator.Direction},
		}

	case EB_Idle:
		return nil, []ElevatorCommand{
			{Type: CmdSetMotorDirection, Value: elevio.MD_Stop},
		}
	}

	return nil, nil
}

// FsmOnCabRequest handles a cab button press that arrives directly from hardware.
// Hall button presses are NEVER handled here — they go to the manager, which
// runs the HRA, achieves network consensus, and pushes back a hall-request
// matrix via FsmOnHallRequestsUpdate.
func FsmOnCabRequest(elevator *Elevator, btnFloor int) ([]elevio.ButtonEvent, []ElevatorCommand) {
	fmt.Printf("\n\nFsmOnCabRequest(floor=%d)\n", btnFloor)
	ElevatorPrint(elevator)

	var served []elevio.ButtonEvent
	var commands []ElevatorCommand

	switch elevator.Behaviour {
	case EB_DoorOpen:
		if CabRequestShouldClearImmediately(elevator, btnFloor) {
			// Already at this floor with doors open — restart door timer.
			commands = append(commands, ElevatorCommand{Type: CmdDoorRequest})
		} else {
			elevator.CabRequests[btnFloor] = true
		}

	case EB_Moving:
		elevator.CabRequests[btnFloor] = true

	case EB_Idle:
		elevator.CabRequests[btnFloor] = true
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return served, commands
}

// FsmOnHallRequestsUpdate replaces the elevator's hall-request matrix with the
// HRA-assigned matrix received from the manager after network consensus.
// If the elevator is idle it acts immediately on any newly assigned requests.
func FsmOnHallRequestsUpdate(elevator *Elevator, newRequests [][2]bool) ([]elevio.ButtonEvent, []ElevatorCommand) {
	fmt.Printf("\n\nFsmOnHallRequestsUpdate()\n")
	ElevatorPrint(elevator)

	replaceHallRequests(elevator, newRequests)

	var served []elevio.ButtonEvent
	var commands []ElevatorCommand

	if elevator.Behaviour == EB_Idle {
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return served, commands
}

func FsmOnFloorArrival(elevator *Elevator, newFloor int) ([]elevio.ButtonEvent, []ElevatorCommand) {
	fmt.Printf("\n\nFsmOnFloorArrival(%d)\n", newFloor)
	ElevatorPrint(elevator)

	elevator.Floor = newFloor
	commands := []ElevatorCommand{
		{Type: CmdSetFloorIndicator, Value: newFloor},
	}

	var served []elevio.ButtonEvent

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			// Initialisation complete, no pending requests — go idle.
			elevator.Behaviour = EB_Idle
			elevator.Direction = elevio.MD_Stop
			commands = append(commands,
				ElevatorCommand{Type: CmdSetMotorDirection, Value: elevio.MD_Stop},
			)

		} else if RequestsShouldStop(elevator) {
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_DoorOpen})
			commands = append(commands, stopCmds...)
		}
		// else: continue moving, nothing extra

	default:
		fmt.Printf("DEBUG: Not moving, no action\n")
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return served, commands
}

// FsmOnDoorClose is called when the door-close event arrives from the door module.
func FsmOnDoorClose(elevator *Elevator) ([]elevio.ButtonEvent, []ElevatorCommand) {
	fmt.Printf("\n\nFsmOnDoorClose()\n")
	ElevatorPrint(elevator)

	switch elevator.Behaviour {
	case EB_DoorOpen:
		served, commands := FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
		fmt.Println("\nNew state:")
		ElevatorPrint(elevator)
		return served, commands

	default:
		fmt.Printf("DEBUG: Doors not open, no action\n")
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return nil, nil
}
