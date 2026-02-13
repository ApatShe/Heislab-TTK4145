package elevatorcontroller

import "Heislab/driver-go/elevio"

// RequestsAbove checks if there are any requests (cab or hall) above the current floor
func RequestsAbove(e *Elevator) bool {
	for f := e.Floor + 1; f < NumFloors; f++ {
		// Check: HallUp || HallDown || Cab
		if e.HallRequests[f][0] || e.HallRequests[f][1] || e.CabRequests[f] {
			return true
		}
	}
	return false
}

// RequestsBelow checks if there are any requests (cab or hall) below the current floor
func RequestsBelow(e *Elevator) bool {
	for f := 0; f < e.Floor; f++ {
		// Check: HallUp || HallDown || Cab
		if e.HallRequests[f][0] || e.HallRequests[f][1] || e.CabRequests[f] {
			return true
		}
	}
	return false
}

// RequestsHere checks if there are requests at the current floor that match the current direction
func RequestsHere(e *Elevator) bool {
	switch e.Dirn {
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
	switch e.Dirn {
	case elevio.MD_Down:
		// Stop if:
		// 1. HallDown request (matches our direction)
		// 2. Cab request
		// 3. HallUp request AND no requests below (might as well pick them up)
		return e.HallRequests[e.Floor][1] ||
			e.CabRequests[e.Floor] ||
			(e.HallRequests[e.Floor][0] && !RequestsBelow(e))

	case elevio.MD_Up:
		// Stop if:
		// 1. HallUp request (matches our direction)
		// 2. Cab request
		// 3. HallDown request AND no requests above (might as well pick them up)
		return e.HallRequests[e.Floor][0] ||
			e.CabRequests[e.Floor] ||
			(e.HallRequests[e.Floor][1] && !RequestsAbove(e))

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

	switch e.Dirn {
	case elevio.MD_Up:
		// Clear HallUp (our direction)
		e.HallRequests[e.Floor][0] = false
		elevio.SetButtonLamp(elevio.BT_HallUp, e.Floor, false)

		// Also clear HallDown if no more requests above
		// (we're about to turn around or stop anyway)
		if !RequestsAbove(e) {
			e.HallRequests[e.Floor][1] = false
			elevio.SetButtonLamp(elevio.BT_HallDown, e.Floor, false)
		}

	case elevio.MD_Down:
		// Clear HallDown (our direction)
		e.HallRequests[e.Floor][1] = false
		elevio.SetButtonLamp(elevio.BT_HallDown, e.Floor, false)

		// Also clear HallUp if no more requests below
		if !RequestsBelow(e) {
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
	switch e.Dirn {
	case elevio.MD_Up:
		// Currently going up
		if RequestsAbove(e) {
			// Continue going up
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if RequestsBelow(e) {
			// Turn around and go down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else {
			// No more requests, become idle
			return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
		}

	case elevio.MD_Down:
		// Currently going down
		if RequestsBelow(e) {
			// Continue going down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else if RequestsAbove(e) {
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
		if RequestsHere(e) {
			// Request at current floor, open doors
			return DirnBehaviourPair{elevio.MD_Stop, EB_DoorOpen}
		} else if RequestsAbove(e) {
			// Requests above, start going up
			return DirnBehaviourPair{elevio.MD_Up, EB_Moving}
		} else if RequestsBelow(e) {
			// Requests below, start going down
			return DirnBehaviourPair{elevio.MD_Down, EB_Moving}
		} else {
			// No requests anywhere, stay idle
			return DirnBehaviourPair{elevio.MD_Stop, EB_Idle}
		}
	}
}
