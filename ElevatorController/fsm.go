package elevatorcontroller

import (
	log "Heislab/Log"
	"Heislab/driver-go/elevio"
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

func FsmOnCabRequests(elevator *Elevator, cabRequests []bool) ([]elevio.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] CabRequestsUpdate: floor=%d dir=%s beh=%s cabRequests=%v", elevator.Floor, DirnToString(elevator.Direction), elevator.Behaviour.String(), cabRequests)

	replaceCabRequests(elevator, cabRequests)

	var served []elevio.ButtonEvent
	var commands []ElevatorCommand

	switch elevator.Behaviour {
	case EB_Idle:
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
	case EB_DoorOpen:
		served = RequestsClearAtCurrentFloor(elevator)
		if len(served) > 0 {
			commands = append(commands, CmdDoorRequestCmd{})
		}
	}

	log.Log("[FSM] New state after cab sync:")
	ElevatorPrint(elevator)
	return served, commands
}

func replaceCabRequests(elevator *Elevator, cabRequests []bool) {
	copy(elevator.CabRequests[:], cabRequests)
}

// FsmOnHallRequestsUpdate replaces the elevator's hall-request matrix with the
// HRA-assigned matrix received from the manager after network consensus.
// If the elevator is idle it acts immediately on any newly assigned requests.
func FsmOnHallRequestsUpdate(elevator *Elevator, newRequests [][2]bool) ([]elevio.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] HallRequestsUpdate: floor=%d dir=%s beh=%s requests=%v", elevator.Floor, DirnToString(elevator.Direction), elevator.Behaviour.String(), newRequests)
	// ElevatorPrint(elevator)

	replaceHallRequests(elevator, newRequests)

	// log.Log("[FSM] Elevator state after hall request update: floor=%d beh=%v requests=%v\n",
	//     elevator.Floor, elevator.Behaviour, elevator.HallRequests)

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

	log.Log("[FSM] New state:")
	ElevatorPrint(elevator)
	return served, commands
}

func FsmOnFloorArrival(elevator *Elevator, newFloor int) ([]elevio.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] Floor arrival: newFloor=%d dir=%s beh=%s", newFloor, DirnToString(elevator.Direction), elevator.Behaviour.String())

	elevator.Floor = newFloor
	commands := []ElevatorCommand{}

	var served []elevio.ButtonEvent

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			log.Log("[FSM] No pending requests — switching to idle")
			// Initialisation complete, no pending requests — go idle.
			var stopCmds []ElevatorCommand
			_, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_Idle})
			commands = append(commands, stopCmds...)

		} else if RequestsShouldStop(elevator) {
			log.Log("[FSM] Requests indicate stop at floor %d — opening door", newFloor)
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_DoorOpen})
			commands = append(commands, stopCmds...)
		}
		// else: continue moving, nothing extra

	default:
		// log.Log("DEBUG: Not moving, no action\n")
	}

	// log.Log("[FSM] New state:")
	// ElevatorPrint(elevator)
	return served, commands
}

// FsmOnDoorClose is called when the door-close event arrives from the door module.
func FsmOnDoorClose(elevator *Elevator) ([]elevio.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] Door close event received: floor=%d dir=%s beh=%s", elevator.Floor, DirnToString(elevator.Direction), elevator.Behaviour.String())

	switch elevator.Behaviour {
	case EB_DoorOpen:
		served, commands := FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
		// log.Log("[FSM] New state:")
		// ElevatorPrint(elevator)
		return served, commands

	default:
		log.Log("[FSM] Door close ignored — behaviour=%s", elevator.Behaviour.String())
	}

	// log.Log("[FSM] New state:")
	// ElevatorPrint(elevator)
	return nil, nil
}
