package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
)

// setAllLights synchronises all button lamps with the current request state.
func setAllLights(elevator *Elevator) {
	// Set cab button lamps
	for f := range NumFloors {
		elevio.SetButtonLamp(elevio.BT_Cab, f, elevator.Requests.CabRequests[f])
		elevio.SetButtonLamp(elevio.BT_HallUp, f, elevator.Requests.HallRequests[f].Up)
		elevio.SetButtonLamp(elevio.BT_HallDown, f, elevator.Requests.HallRequests[f].Down)
	}
}
