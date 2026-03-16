package lights

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/driver-go/elevio"
	"Heislab/manager"
)

// RunLights is the single point of contact for all button and indicator lamps.
// It owns three concerns:
//
//   - Cab lights and floor indicator — driven by the local elevator FSM state.
//     These reflect what this elevator has committed to serve.
//
//   - Hall lights: driven by the consensus hall-request matrix from the manager.
//     A hall light is only switched on when all peers have acknowledged the request
//     (ACTIVE), and only switched off when consensus says it has been cleared.
//     RunLights does not decide when that happens, it only executes the command.
//
//   - Door lamp: driven by RunDoor via in.DoorLamp. RunDoor owns door state;
//     RunLights owns the lamp.

// LightsIn groups all channels that deliver display-state updates into RunLights.
type LightsIn struct {
	ElevatorState <-chan elevatorcontroller.Elevator
	RequestLights <-chan manager.RequestLights
	DoorLamp      <-chan bool
}

func RunLights(in LightsIn) {
	for {
		select {
		case elevator := <-in.ElevatorState:

			if elevator.Floor >= 0 {
				elevio.SetFloorIndicator(elevator.Floor)
			}

		case lights := <-in.RequestLights:
			setHallLights(lights.HallLights)
			setCabLights(lights.CabLights)

		case open := <-in.DoorLamp:
			elevio.SetDoorOpenLamp(open)
		}
	}
}

func setCabLights(cabRequests []bool) {
	for floor, active := range cabRequests {
		elevio.SetButtonLamp(elevio.BT_Cab, floor, active)
	}
}

func setHallLights(hallRequests [][2]bool) {
	for floor, btnPair := range hallRequests {
		elevio.SetButtonLamp(elevio.BT_HallUp, floor, btnPair[elevatorcontroller.HallUp])
		elevio.SetButtonLamp(elevio.BT_HallDown, floor, btnPair[elevatorcontroller.HallDown])
	}
}
