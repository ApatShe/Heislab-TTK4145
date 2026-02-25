package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

// FsmOnInitBetweenFloors handles the case where the elevator starts
// between floors: it drives downward until reaching a known floor.
func FsmOnInitBetweenFloors(e *Elevator) {
	fmt.Printf("\n\nFsmOnInitBetweenFloors()\n")
	ElevatorPrint(e)

	elevio.SetMotorDirection(elevio.MD_Down)
	e.Direction = elevio.MD_Down
	e.Behaviour = EB_Moving
	fmt.Printf("DEBUG: Set motor to Down, Behaviour to Moving\n")

	fmt.Println("\nNew state:")
	ElevatorPrint(e)
}

// FsmOnRequestButtonPress handles a new button press event.
// CAB buttons are handled locally, HALL buttons should be sent to Network/Manager.
func FsmOnRequestButtonPress(e *Elevator, btnFloor int, btnType elevio.ButtonType, timer *DoorTimer) {
	fmt.Printf("\n\nFsmOnRequestButtonPress(%d, %s)\n", btnFloor, ButtonToString(btnType))
	ElevatorPrint(e)

	// Route button press to appropriate storage
	switch btnType {
	case elevio.BT_Cab:
		fmt.Printf("DEBUG: Handling cab button\n")
		// Handle cab button locally
		switch e.Behaviour {
		case EB_DoorOpen:
			fmt.Printf("DEBUG: Doors open, checking if should clear immediately\n")
			if RequestsShouldClearImmediately(e, btnFloor) {
				fmt.Printf("DEBUG: Clearing immediately, restarting timer\n")
				timer.Start(e.Config.DoorOpenDuration)
			} else {
				fmt.Printf("DEBUG: Adding cab request at floor %d\n", btnFloor)
				e.CabRequests[btnFloor] = true
			}

		case EB_Moving:
			fmt.Printf("DEBUG: Moving, adding cab request at floor %d\n", btnFloor)
			e.CabRequests[btnFloor] = true

		case EB_Idle:
			fmt.Printf("DEBUG: Idle, adding cab request at floor %d\n", btnFloor)
			e.CabRequests[btnFloor] = true
			pair := RequestsChooseDirection(e)
			e.Direction = pair.Direction
			e.Behaviour = pair.Behaviour
			fmt.Printf("DEBUG: Chose Direction=%s, Behaviour=%s\n", DirectionToString(e.Direction), e.Behaviour.String())

			switch pair.Behaviour {
			case EB_DoorOpen:
				fmt.Printf("DEBUG: Opening doors at current floor\n")
				elevio.SetDoorOpenLamp(true)
				timer.Start(e.Config.DoorOpenDuration)
				RequestsClearAtCurrentFloor(e)

			case EB_Moving:
				fmt.Printf("DEBUG: Starting movement\n")
				elevio.SetMotorDirection(e.Direction)

			case EB_Idle:
				fmt.Printf("DEBUG: Staying idle\n")
			}
		}

	case elevio.BT_HallUp, elevio.BT_HallDown:
		fmt.Printf("DEBUG: Hall button - forwarding to Manager\n")
		// Hall buttons should be sent to Network/Manager for distribution
		// They will eventually come back via UpdateHallRequests()
		return
	}

	setAllLights(e)

	fmt.Println("\nNew state:")
	ElevatorPrint(e)
}

// FsmOnFloorArrival handles the elevator arriving at a new floor.
func FsmOnFloorArrival(e *Elevator, newFloor int, timer *DoorTimer) {
	fmt.Printf("\n\nFsmOnFloorArrival(%d)\n", newFloor)
	ElevatorPrint(e)

	e.Floor = newFloor
	elevio.SetFloorIndicator(e.Floor)
	fmt.Printf("DEBUG: Arrived at floor %d, set indicator\n", newFloor)

	switch e.Behaviour {
	case EB_Moving:
		fmt.Printf("DEBUG: Was moving, checking if should stop\n")

		shouldStop := RequestsShouldStop(e)

		// Safety fix: If no requests anywhere, force stop (handles initialization case)
		if !shouldStop && !RequestsAbove(e) && !RequestsBelow(e) && !RequestsHere(e) {
			fmt.Printf("DEBUG: No requests anywhere (Init complete?), forcing stop\n")
			elevio.SetMotorDirection(elevio.MD_Stop)
			e.Behaviour = EB_Idle
			// Don't open doors or clear requests, just go idle
		} else if shouldStop {
			fmt.Printf("DEBUG: Should stop, stopping motor and opening doors\n")
			elevio.SetMotorDirection(elevio.MD_Stop)
			elevio.SetDoorOpenLamp(true)
			timer.Start(e.Config.DoorOpenDuration)
			e.Behaviour = EB_DoorOpen
			RequestsClearAtCurrentFloor(e)
		} else {
			fmt.Printf("DEBUG: Should not stop, continuing\n")
		}

	default:
		fmt.Printf("DEBUG: Not moving, no action\n")
	}

	setAllLights(e)

	fmt.Println("\nNew state:")
	ElevatorPrint(e)
}

// FsmOnDoorTimeout handles the door-open timer expiring.
func FsmOnDoorTimeout(e *Elevator, timer *DoorTimer) {
	fmt.Printf("\n\nFsmOnDoorTimeout()\n")
	ElevatorPrint(e)

	if e.ObstructionActive {
		fmt.Printf("DEBUG: Obstruction active, restarting timer\n")
		timer.Start(e.Config.DoorOpenDuration)
		return
	}

	switch e.Behaviour {
	case EB_DoorOpen:
		fmt.Printf("DEBUG: Doors were open, choosing next action\n")
		pair := RequestsChooseDirection(e)
		e.Direction = pair.Direction
		e.Behaviour = pair.Behaviour
		fmt.Printf("DEBUG: Chose Direction=%s, Behaviour=%s\n", DirectionToString(e.Direction), e.Behaviour.String())

		switch pair.Behaviour {
		case EB_DoorOpen:
			fmt.Printf("DEBUG: Keeping doors open, restarting timer\n")
			timer.Start(e.Config.DoorOpenDuration)
			RequestsClearAtCurrentFloor(e)

		case EB_Moving:
			fmt.Printf("DEBUG: Closing doors and starting movement\n")
			elevio.SetDoorOpenLamp(false)
			elevio.SetMotorDirection(e.Direction)
			fmt.Printf("DEBUG: FSM Starting movement dir=%v\n", e.Direction)

		case EB_Idle:
			fmt.Printf("DEBUG: Closing doors and becoming idle\n")
			elevio.SetDoorOpenLamp(false)
			elevio.SetMotorDirection(elevio.MD_Stop)
		}
	default:
		fmt.Printf("DEBUG: Doors not open, no action\n")
	}

	setAllLights(e)

	fmt.Println("\nNew state:")
	ElevatorPrint(e)
}
