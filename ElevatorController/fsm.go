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
		return RequestsClearAtCurrentFloor(elevator), []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevio.MD_Stop}, // ← add this
			CmdDoorRequestCmd{},
		}

	case EB_Moving:
		return nil, []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevator.Direction},
		}

	case EB_Idle:
		return nil, []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevio.MD_Stop},
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
			commands = append(commands, CmdDoorRequestCmd{})
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
	fmt.Printf("[FSM] HallRequestsUpdate called, floor=%d beh=%v requests=%v\n",
		elevator.Floor, elevator.Behaviour, newRequests)
	ElevatorPrint(elevator)

	replaceHallRequests(elevator, newRequests)

	var served []elevio.ButtonEvent
	var commands []ElevatorCommand

	switch elevator.Behaviour {
	case EB_Idle:
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))

	case EB_DoorOpen:
		// General fix: any request assigned at our current floor while doors
		// are open should be served immediately and the door timer restarted.
		// RequestsClearAtCurrentFloor already knows direction, floor, and all
		// request types — this covers every case, not just the HallDown race.
		served = RequestsClearAtCurrentFloor(elevator)
		if len(served) > 0 {
			commands = append(commands, CmdDoorRequestCmd{})
		}
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
		CmdSetFloorIndicatorCmd{Floor: newFloor},
	}

	var served []elevio.ButtonEvent

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			// Initialisation complete, no pending requests — go idle.
			var stopCmds []ElevatorCommand
			_, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_Idle})
			commands = append(commands, stopCmds...)

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
