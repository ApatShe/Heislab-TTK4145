package elevatorcontroller

import (
	log "Heislab/Log"
	elevatordriver "Heislab/elevatordriver"
)

// FsmActOnBehaviourPair writes direction+behaviour into elevator and returns
// the hardware commands to execute alongside any served hall requests.
// Only caller of RequestsClearAtCurrentFloor.
func FsmActOnBehaviourPair(elevator *Elevator, pair DirnBehaviourPair) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {
	elevator.Direction = pair.Direction
	elevator.Behaviour = pair.Behaviour

	switch pair.Behaviour {
	case EB_DoorOpen:
		return RequestsClearAtCurrentFloor(elevator), []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevatordriver.MD_Stop},
			CmdDoorRequestCmd{},
		}

	case EB_Moving:
		return nil, []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevator.Direction},
		}

	case EB_Idle:
		return nil, []ElevatorCommand{
			CmdSetMotorDirectionCmd{Dir: elevatordriver.MD_Stop},
		}
	}

	return nil, nil
}

func FsmOnCabRequests(elevator *Elevator, cabRequests []bool) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] CabRequestsUpdate: floor=%d dir=%s beh=%s cabRequests=%v", elevator.Floor, DirnToString(elevator.Direction), elevator.Behaviour.String(), cabRequests)

	replaceCabRequests(elevator, cabRequests)

	var served []elevatordriver.ButtonEvent
	var commands []ElevatorCommand

	switch elevator.Behaviour {
	case EB_Idle:
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
	case EB_DoorOpen:
		if elevator.CabRequests[elevator.Floor] {
			elevator.CabRequests[elevator.Floor] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_Cab})
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
// HRA-assigned matrix received from the coordinator after network consensus.
// If the elevator is idle it acts immediately on any newly assigned requests.
func FsmOnHallRequestsUpdate(elevator *Elevator, newRequests [][2]bool) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] HallRequestsUpdate: floor=%d dir=%s beh=%s requests=%v", elevator.Floor, DirnToString(elevator.Direction), elevator.Behaviour.String(), newRequests)

	replaceHallRequests(elevator, newRequests)

	var served []elevatordriver.ButtonEvent
	var commands []ElevatorCommand

	switch elevator.Behaviour {
	case EB_Idle:
		served, commands = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
	case EB_DoorOpen:
		hasInDirectionMatch := false
		switch elevator.Direction {
		case elevatordriver.MD_Up:
			hasInDirectionMatch = elevator.HallRequests[elevator.Floor][HallUp] ||
				elevator.CabRequests[elevator.Floor]
		case elevatordriver.MD_Down:
			hasInDirectionMatch = elevator.HallRequests[elevator.Floor][HallDown] ||
				elevator.CabRequests[elevator.Floor]
		default:
			hasInDirectionMatch = elevator.HallRequests[elevator.Floor][HallUp] ||
				elevator.HallRequests[elevator.Floor][HallDown] ||
				elevator.CabRequests[elevator.Floor]
		}
		if hasInDirectionMatch {
			served = RequestsClearAtCurrentFloor(elevator)
			if len(served) > 0 {
				commands = append(commands, CmdDoorRequestCmd{})
			}
		}
	}

	log.Log("[FSM] New state:")
	ElevatorPrint(elevator)
	return served, commands
}

func FsmOnFloorArrival(elevator *Elevator, newFloor int) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {
	log.Log("[FSM] Floor arrival: newFloor=%d dir=%s beh=%s", newFloor, DirnToString(elevator.Direction), elevator.Behaviour.String())

	elevator.Floor = newFloor
	commands := []ElevatorCommand{}
	var served []elevatordriver.ButtonEvent

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			log.Log("[FSM] No pending requests — switching to idle")
			_, stopCmds := FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_Idle})
			commands = append(commands, stopCmds...)

		} else if RequestsShouldStop(elevator) {
			log.Log("[FSM] Requests indicate stop at floor %d — opening door", newFloor)
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_DoorOpen})
			commands = append(commands, stopCmds...)

		} else if elevator.Direction == elevatordriver.MD_Down && !RequestsBelow(elevator) && RequestsAbove(elevator) {
			log.Log("[FSM] Started between floors moving down but all requests are above — reversing")
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
			commands = append(commands, stopCmds...)
		}
		// else: continue moving, nothing extra

	default:
	}

	return served, commands
}

// FsmOnDoorClose is called when the door-close event arrives from the door module.
func FsmOnDoorClose(elevator *Elevator) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {
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
