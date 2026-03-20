package elevatorcontroller

import (
	elevatordriver "Heislab/elevatordriver"
)

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

	ElevatorPrint(elevator)
	return served, commands
}

func replaceCabRequests(elevator *Elevator, cabRequests []bool) {
	copy(elevator.CabRequests[:], cabRequests)
}

func FsmOnHallRequestsUpdate(elevator *Elevator, newRequests [][2]bool) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {

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

	ElevatorPrint(elevator)
	return served, commands
}

func FsmOnFloorArrival(elevator *Elevator, newFloor int) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {

	elevator.Floor = newFloor
	commands := []ElevatorCommand{}
	var served []elevatordriver.ButtonEvent

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			_, stopCmds := FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_Idle})
			commands = append(commands, stopCmds...)

		} else if RequestsShouldStop(elevator) {
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_DoorOpen})
			commands = append(commands, stopCmds...)

		} else if elevator.Direction == elevatordriver.MD_Down && !RequestsBelow(elevator) && RequestsAbove(elevator) {
			var stopCmds []ElevatorCommand
			served, stopCmds = FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
			commands = append(commands, stopCmds...)
		}

	default:
	}

	return served, commands
}

func FsmOnDoorClose(elevator *Elevator) ([]elevatordriver.ButtonEvent, []ElevatorCommand) {

	switch elevator.Behaviour {
	case EB_DoorOpen:
		served, commands := FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator))
		return served, commands

	default:
	}

	return nil, nil
}
