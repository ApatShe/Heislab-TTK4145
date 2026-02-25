package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

func ClearAllCabRequests(elevator *Elevator) {
	for f := 0; f < NumFloors; f++ {
		elevator.CabRequests[f] = false
	}
}

// TODO: clearHall requests, typically cleared too or re-assigned

/* not in use
func hasNewRequests(old, new_ [NumFloors][2]bool) bool {
	for f := 0; f < NumFloors; f++ {
		if (new_[f][0] && !old[f][0]) || (new_[f][1] && !old[f][1]) {
			return true
		}
	}
	return false
}
*/
//TODO: eplaceHallRequests, ClearRequestsAtCurrentFloor,
func ReplaceHallRequests(newHallRequests [4][2]bool) bool {
	fmt.Println("Replacing hall request has not been implemented", newHallRequests)
	return false
}

// RequestsAbove checks if there are any requests (cab or hall) above the current floor
func CheckRequestsAbove(e *Elevator) bool {
	for f := e.Floor + 1; f < NumFloors; f++ {
		// Check: HallUp || HallDown || Cab
		if e.HallRequests[f][HallUp] || e.HallRequests[f][HallDown] || e.CabRequests[f] {
			return true
		}
	}
	return false
}

// RequestsBelow checks if there are any requests (cab or hall) below the current floor
func CheckRequestsBelow(e *Elevator) bool {
	for f := 0; f < e.Floor; f++ {
		// Check: HallUp || HallDown || Cab
		if e.HallRequests[f][0] || e.HallRequests[f][1] || e.CabRequests[f] {
			return true
		}
	}
	return false
}

// RequestsHere checks if there are requests at the current floor that match the current direction
func CheckIfRequestsAtCurrentFloor(e *Elevator) bool {
	switch e.Direction {
	case elevio.MD_Up:
		// Going up: check HallUp or Cab
		return e.HallRequests[e.Floor][0] || e.CabRequests[e.Floor]

	case elevio.MD_Down:
		// Going down: check HallDown or Cab
		return e.HallRequests[e.Floor][1] || e.CabRequests[e.Floor]

	case elevio.MD_Stop:
		fallthrough
	default:
		// Stopped/Idle: check any request at this floor
		return e.HallRequests[e.Floor][0] || e.HallRequests[e.Floor][1] || e.CabRequests[e.Floor]
	}
}

// RequestsShouldStop determines if the elevator should stop at the current floor
// This implements the classic elevator collective behavior algorithm
func RequestsShouldStop(e *Elevator) bool {
	switch e.Direction {
	case elevio.MD_Down:
		// Stop if:
		// 1. HallDown request (matches our direction)
		// 2. Cab request
		// 3. HallUp request AND no requests below (might as well pick them up)
		return e.HallRequests[e.Floor][1] ||
			e.CabRequests[e.Floor] ||
			(e.HallRequests[e.Floor][0] && !CheckRequestsBelow(e))

	case elevio.MD_Up:
		// Stop if:
		// 1. HallUp request (matches our direction)
		// 2. Cab request
		// 3. HallDown request AND no requests above (might as well pick them up)
		return e.HallRequests[e.Floor][0] ||
			e.CabRequests[e.Floor] ||
			(e.HallRequests[e.Floor][1] && !CheckRequestsAbove(e))

	case elevio.MD_Stop:
		fallthrough
	default:
		// When stopped, stop for any request
		return true
	}

}

// RequestsShouldClearImmediately checks if a cab button press should immediately clear
// (i.e., if we're already at that floor with doors open)
func RequestsShouldClearImmediately(e *Elevator, btnFloor int) bool {
	// Only makes sense for cab buttons, and only if we're at that floor
	return e.Floor == btnFloor && e.Behaviour == EB_DoorOpen
}

//TODO: THIS MAY NEED SIGNIFICANT CHANGE IN ORDER TO RESPECT THE BUTTON HALL BUTTON CONTRACT

// RequestsClearAtCurrentFloor clears all serviced requests at the current floor
// Implements the collective behavior clearing logic
func RequestsClearAtCurrentFloor(e *Elevator) {
	// Always clear cab request at current floor
	e.CabRequests[e.Floor] = false
	elevio.SetButtonLamp(elevio.BT_Cab, e.Floor, false)

	// Clear hall requests based on direction and remaining requests

	switch e.Direction {
	case elevio.MD_Up:
		// Clear HallUp (our direction)
		e.HallRequests[e.Floor][0] = false
		elevio.SetButtonLamp(elevio.BT_HallUp, e.Floor, false)

		// Also clear HallDown if no more requests above
		// (we're about to turn around or stop anyway)
		if !CheckRequestsAbove(e) {
			e.HallRequests[e.Floor][1] = false
			elevio.SetButtonLamp(elevio.BT_HallDown, e.Floor, false)
		}

	case elevio.MD_Down:
		// Clear HallDown (our direction)
		e.HallRequests[e.Floor][1] = false
		elevio.SetButtonLamp(elevio.BT_HallDown, e.Floor, false)

		// Also clear HallUp if no more requests below
		if !CheckRequestsBelow(e) {
			e.HallRequests[e.Floor][0] = false
			elevio.SetButtonLamp(elevio.BT_HallUp, e.Floor, false)
		}

	case elevio.MD_Stop:
	default:
		// When stopped/idle, clear all hall requests at this floor
		e.HallRequests[e.Floor][0] = false
		e.HallRequests[e.Floor][1] = false
		elevio.SetButtonLamp(elevio.BT_HallUp, e.Floor, false)
		elevio.SetButtonLamp(elevio.BT_HallDown, e.Floor, false)
	}
}

// RequestsChooseDirection determines the next direction and behavior based on current requests
// This is the core elevator movement decision algorithm
func RequestsChooseDirection(e *Elevator) DirnBehaviourPair {
	switch e.Direction {
	case elevio.MD_Up:
		// Currently going up
		if CheckRequestsAbove(e) {
			// Continue going up
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if CheckRequestsBelow(e) {
			// Turn around and go down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else {
			// No more requests, become idle
			return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
		}

	case elevio.MD_Down:
		// Currently going down
		if CheckRequestsBelow(e) {
			// Continue going down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else if CheckRequestsAbove(e) {
			// Turn around and go up
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else {
			// No more requests, become idle
			return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
		}

	case elevio.MD_Stop:
		fallthrough

	default:
		// Currently stopped/idle
		if CheckIfRequestsAtCurrentFloor(e) {
			// Request at current floor, open doors
			return DirnBehaviourPair{elevio.MD_Stop, EB_DoorOpen}
		} else if CheckRequestsAbove(e) {
			// Requests above, start going up
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if CheckRequestsBelow(e) {
			// Requests below, start going down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else {
			// No requests anywhere, stay idle
			return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
		}
	}
}
