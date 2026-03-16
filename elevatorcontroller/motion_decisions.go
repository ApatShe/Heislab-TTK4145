package elevatorcontroller

import (
	log "Heislab/Log"
	elevatordriver "Heislab/elevatordriver"
)

type DirnBehaviourPair struct {
	Direction elevatordriver.MotorDirection
	Behaviour ElevatorBehaviour
}

func RequestsAbove(elevator *Elevator) bool {
	for f := elevator.Floor + 1; f < NumFloors; f++ {
		if elevator.HallRequests[f][HallUp] || elevator.HallRequests[f][HallDown] || elevator.CabRequests[f] {
			return true
		}
	}
	return false
}

func RequestsBelow(elevator *Elevator) bool {
	for f := 0; f < elevator.Floor; f++ {
		if elevator.HallRequests[f][HallUp] || elevator.HallRequests[f][HallDown] || elevator.CabRequests[f] {
			return true
		}
	}
	return false
}

func RequestsHere(elevator *Elevator) bool {
	switch elevator.Direction {
	case elevatordriver.MD_Up:
		return elevator.HallRequests[elevator.Floor][HallUp] || elevator.CabRequests[elevator.Floor]
	case elevatordriver.MD_Down:
		return elevator.HallRequests[elevator.Floor][HallDown] || elevator.CabRequests[elevator.Floor]
	default:
		return elevator.HallRequests[elevator.Floor][HallUp] || elevator.HallRequests[elevator.Floor][HallDown] || elevator.CabRequests[elevator.Floor]
	}
}

func RequestsShouldStop(elevator *Elevator) bool {
	switch elevator.Direction {
	case elevatordriver.MD_Down:
		return elevator.HallRequests[elevator.Floor][HallDown] ||
			elevator.CabRequests[elevator.Floor] ||
			(elevator.HallRequests[elevator.Floor][HallUp] && !RequestsBelow(elevator))
	case elevatordriver.MD_Up:
		return elevator.HallRequests[elevator.Floor][HallUp] ||
			elevator.CabRequests[elevator.Floor] ||
			(elevator.HallRequests[elevator.Floor][HallDown] && !RequestsAbove(elevator))
	default:
		return true
	}
}

func HasNoRequests(elevator *Elevator) bool {
	for f := 0; f < NumFloors; f++ {
		if elevator.CabRequests[f] || elevator.HallRequests[f][0] || elevator.HallRequests[f][1] {
			return false
		}
	}
	return true
}

func CabRequestShouldClearImmediately(elevator *Elevator, btnFloor int) bool {
	return elevator.Floor == btnFloor && elevator.Behaviour == EB_DoorOpen
}

// RequestsClearAtCurrentFloor clears served requests and returns cleared hall
// requests so RunElevator can notify the Manager via servedChan.
// No lamp calls — RunLights owns all lamps via elevator state broadcast.
func RequestsClearAtCurrentFloor(elevator *Elevator) []elevatordriver.ButtonEvent {
	served := []elevatordriver.ButtonEvent{}

	if elevator.CabRequests[elevator.Floor] {
		elevator.CabRequests[elevator.Floor] = false
		served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_Cab})
	}

	switch elevator.Direction {
	case elevatordriver.MD_Up:
		if elevator.HallRequests[elevator.Floor][HallUp] {
			elevator.HallRequests[elevator.Floor][HallUp] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallUp})
		}
		if !RequestsAbove(elevator) && elevator.HallRequests[elevator.Floor][HallDown] {
			elevator.HallRequests[elevator.Floor][HallDown] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallDown})
		}
	case elevatordriver.MD_Down:
		if elevator.HallRequests[elevator.Floor][HallDown] {
			elevator.HallRequests[elevator.Floor][HallDown] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallDown})
		}
		if !RequestsBelow(elevator) && elevator.HallRequests[elevator.Floor][HallUp] {
			elevator.HallRequests[elevator.Floor][HallUp] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallUp})
		}
	default:
		if elevator.HallRequests[elevator.Floor][HallUp] {
			elevator.HallRequests[elevator.Floor][HallUp] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallUp})
		}
		if elevator.HallRequests[elevator.Floor][HallDown] {
			elevator.HallRequests[elevator.Floor][HallDown] = false
			served = append(served, elevatordriver.ButtonEvent{Floor: elevator.Floor, Button: elevatordriver.BT_HallDown})
		}
	}
	return served
}

// replaceHallRequests writes the Manager-assigned hall matrix into the elevator.
func replaceHallRequests(elevator *Elevator, newRequests [][2]bool) {
	// log.Log("[FSM] Replacing hall request matrix %v with manager assignment: %v", elevator.HallRequests, newRequests)
	for f := 0; f < NumFloors; f++ {
		elevator.HallRequests[f][HallUp] = newRequests[f][HallUp]
		elevator.HallRequests[f][HallDown] = newRequests[f][HallDown]
	}
}

func RequestsChooseDirection(elevator *Elevator) DirnBehaviourPair {
	log.Log("[FSM] ChooseDirection: floor=%d dir=%s above=%v below=%v here=%v", elevator.Floor, DirnToString(elevator.Direction), RequestsAbove(elevator), RequestsBelow(elevator), RequestsHere(elevator))
	switch elevator.Direction {
	case elevatordriver.MD_Up:
		if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Up, EB_Moving}
		} else if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Down, EB_Moving}
		}
		return DirnBehaviourPair{elevatordriver.MD_Stop, EB_Idle}

	case elevatordriver.MD_Down:
		if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Down, EB_Moving}
		} else if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Up, EB_Moving}
		}
		return DirnBehaviourPair{elevatordriver.MD_Stop, EB_Idle}

	default:
		if RequestsHere(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Stop, EB_DoorOpen}
		} else if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Up, EB_Moving}
		} else if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevatordriver.MD_Down, EB_Moving}
		}
		return DirnBehaviourPair{elevatordriver.MD_Stop, EB_Idle}
	}
}
