package elevatorcontroller

import "Heislab/driver-go/elevio"

type DirnBehaviourPair struct {
	Direction elevio.MotorDirection
	Behaviour ElevatorBehaviour
}

func RequestsAbove(elevator *Elevator) bool {
	for f := elevator.Floor + 1; f < NumFloors; f++ {
		if elevator.HallRequests[f][0] || elevator.HallRequests[f][1] || elevator.CabRequests[f] {
			return true
		}
	}
	return false
}

func RequestsBelow(elevator *Elevator) bool {
	for f := 0; f < elevator.Floor; f++ {
		if elevator.HallRequests[f][0] || elevator.HallRequests[f][1] || elevator.CabRequests[f] {
			return true
		}
	}
	return false
}

func RequestsHere(elevator *Elevator) bool {
	switch elevator.Direction {
	case elevio.MD_Up:
		return elevator.HallRequests[elevator.Floor][0] || elevator.CabRequests[elevator.Floor]
	case elevio.MD_Down:
		return elevator.HallRequests[elevator.Floor][1] || elevator.CabRequests[elevator.Floor]
	default:
		return elevator.HallRequests[elevator.Floor][0] || elevator.HallRequests[elevator.Floor][1] || elevator.CabRequests[elevator.Floor]
	}
}

func RequestsShouldStop(elevator *Elevator) bool {
	switch elevator.Direction {
	case elevio.MD_Down:
		return elevator.HallRequests[elevator.Floor][1] ||
			elevator.CabRequests[elevator.Floor] ||
			(elevator.HallRequests[elevator.Floor][0] && !RequestsBelow(elevator))
	case elevio.MD_Up:
		return elevator.HallRequests[elevator.Floor][0] ||
			elevator.CabRequests[elevator.Floor] ||
			(elevator.HallRequests[elevator.Floor][1] && !RequestsAbove(elevator))
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
func RequestsClearAtCurrentFloor(elevator *Elevator) []elevio.ButtonEvent {
	served := []elevio.ButtonEvent{}

	elevator.CabRequests[elevator.Floor] = false

	switch elevator.Direction {
	case elevio.MD_Up:
		if elevator.HallRequests[elevator.Floor][0] {
			elevator.HallRequests[elevator.Floor][0] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallUp})
		}
		if !RequestsAbove(elevator) && elevator.HallRequests[elevator.Floor][1] {
			elevator.HallRequests[elevator.Floor][1] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallDown})
		}
	case elevio.MD_Down:
		if elevator.HallRequests[elevator.Floor][1] {
			elevator.HallRequests[elevator.Floor][1] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallDown})
		}
		if !RequestsBelow(elevator) && elevator.HallRequests[elevator.Floor][0] {
			elevator.HallRequests[elevator.Floor][0] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallUp})
		}
	default:
		if elevator.HallRequests[elevator.Floor][0] {
			elevator.HallRequests[elevator.Floor][0] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallUp})
		}
		if elevator.HallRequests[elevator.Floor][1] {
			elevator.HallRequests[elevator.Floor][1] = false
			served = append(served, elevio.ButtonEvent{Floor: elevator.Floor, Button: elevio.BT_HallDown})
		}
	}

	return served
}

// replaceHallRequests writes the Manager-assigned hall matrix into the elevator.
func replaceHallRequests(elevator *Elevator, newRequests [][2]bool) {
	for f := 0; f < NumFloors; f++ {
		elevator.HallRequests[f][0] = newRequests[f][0]
		elevator.HallRequests[f][1] = newRequests[f][1]
	}
}

func RequestsChooseDirection(elevator *Elevator) DirnBehaviourPair {
	switch elevator.Direction {
	case elevio.MD_Up:
		if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		}
		return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}

	case elevio.MD_Down:
		if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		}
		return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}

	default:
		if RequestsHere(elevator) {
			return DirnBehaviourPair{elevio.MD_Stop, EB_DoorOpen}
		} else if RequestsAbove(elevator) {
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if RequestsBelow(elevator) {
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		}
		return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
	}
}
