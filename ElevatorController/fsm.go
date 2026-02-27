package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

// FsmActOnBehaviourPair writes direction+behaviour into elevator and executes
// the corresponding hardware action. Returns served hall requests for Manager.
// Only caller of RequestsClearAtCurrentFloor.
func FsmActOnBehaviourPair(elevator *Elevator, pair DirnBehaviourPair, timer *DoorTimer) []elevio.ButtonEvent {
	elevator.Direction = pair.Direction
	elevator.Behaviour = pair.Behaviour

	switch pair.Behaviour {
	case EB_DoorOpen:
		elevio.SetDoorOpenLamp(true)
		timer.Start(elevator.Config.DoorOpenDuration)
		return RequestsClearAtCurrentFloor(elevator)

	case EB_Moving:
		elevio.SetMotorDirection(elevator.Direction)

	case EB_Idle:
		elevio.SetMotorDirection(elevio.MD_Stop)
	}

	return nil
}

func FsmOnInitBetweenFloors(elevator *Elevator) {
	fmt.Printf("\n\nFsmOnInitBetweenFloors()\n")
	ElevatorPrint(elevator)

	elevio.SetMotorDirection(elevio.MD_Down)
	elevator.Direction = elevio.MD_Down
	elevator.Behaviour = EB_Moving

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
}

// CabRequestButtonPress handles a cab button press.
// Hall buttons never reach here — RunElevator routes them to orderChan.
func CabRequestButtonPress(elevator *Elevator, btnFloor int, timer *DoorTimer) []elevio.ButtonEvent {
	fmt.Printf("\n\nCabRequestButtonPress(%d)\n", btnFloor)
	ElevatorPrint(elevator)

	switch elevator.Behaviour {
	case EB_DoorOpen:
		if CabRequestShouldClearImmediately(elevator, btnFloor) {
			timer.Start(elevator.Config.DoorOpenDuration)
		} else {
			elevator.CabRequests[btnFloor] = true
		}

	case EB_Moving:
		elevator.CabRequests[btnFloor] = true

	case EB_Idle:
		elevator.CabRequests[btnFloor] = true
		return FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator), timer)
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return nil
}

func FsmOnFloorArrival(elevator *Elevator, newFloor int, timer *DoorTimer) []elevio.ButtonEvent {
	fmt.Printf("\n\nFsmOnFloorArrival(%d)\n", newFloor)
	ElevatorPrint(elevator)

	elevator.Floor = newFloor

	switch elevator.Behaviour {
	case EB_Moving:
		if HasNoRequests(elevator) {
			// Init just completed, no requests yet — just go idle
			elevio.SetMotorDirection(elevio.MD_Stop)
			elevator.Behaviour = EB_Idle

		} else if RequestsShouldStop(elevator) {
			elevio.SetMotorDirection(elevio.MD_Stop)
			served := FsmActOnBehaviourPair(elevator, DirnBehaviourPair{elevator.Direction, EB_DoorOpen}, timer)
			fmt.Println("\nNew state:")
			ElevatorPrint(elevator)
			return served
		}
		// else: continue moving, no action

	default:
		fmt.Printf("DEBUG: Not moving, no action\n")
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return nil
}

func FsmOnDoorTimeout(elevator *Elevator, timer *DoorTimer) []elevio.ButtonEvent {
	fmt.Printf("\n\nFsmOnDoorTimeout()\n")
	ElevatorPrint(elevator)

	if elevator.ObstructionActive {
		timer.Start(elevator.Config.DoorOpenDuration)
		return nil
	}

	switch elevator.Behaviour {
	case EB_DoorOpen:
		elevio.SetDoorOpenLamp(false)
		served := FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator), timer)
		fmt.Println("\nNew state:")
		ElevatorPrint(elevator)
		return served

	default:
		fmt.Printf("DEBUG: Doors not open, no action\n")
	}

	fmt.Println("\nNew state:")
	ElevatorPrint(elevator)
	return nil
}
