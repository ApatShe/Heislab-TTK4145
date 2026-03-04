package lights

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/driver-go/elevio"
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
//   - Door lamp: driven by RunDoor via doorLampChan. RunDoor owns door state;
//     RunLights owns the lamp.

func RunLights(
	lightsElevatorStateChan <-chan elevatorcontroller.Elevator,
	hallLightsChan <-chan [][2]bool,
	doorLampChan <-chan bool,
) {
	for {
		select {
		case elevator := <-lightsElevatorStateChan:

			setCabLights(elevator.CabRequests[:])
			elevio.SetFloorIndicator(elevator.Floor)

		case hallRequests := <-hallLightsChan:
			setHallLights(hallRequests)

		case open := <-doorLampChan:
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
		elevio.SetButtonLamp(elevio.BT_HallUp, floor, btnPair[0])
		elevio.SetButtonLamp(elevio.BT_HallDown, floor, btnPair[1])
	}
}
