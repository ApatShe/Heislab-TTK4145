package lights

import (
	"Heislab/coordinator"
	elevatorcontroller "Heislab/elevatorcontroller"
	elevatordriver "Heislab/elevatordriver"
)

type LightsIn struct {
	ElevatorState <-chan elevatorcontroller.Elevator
	RequestLights <-chan coordinator.RequestLights
	DoorLamp      <-chan bool
}

func RunLights(in LightsIn) {
	for {
		select {
		case elevator := <-in.ElevatorState:

			if elevator.Floor >= 0 {
				elevatordriver.SetFloorIndicator(elevator.Floor)
			}

		case lights := <-in.RequestLights:
			setHallLights(lights.HallLights)
			setCabLights(lights.CabLights)

		case open := <-in.DoorLamp:
			elevatordriver.SetDoorOpenLamp(open)
		}
	}
}

func setCabLights(cabRequests []bool) {
	for floor, active := range cabRequests {
		elevatordriver.SetButtonLamp(elevatordriver.BT_Cab, floor, active)
	}
}

func setHallLights(hallRequests [][2]bool) {
	for floor, btnPair := range hallRequests {
		elevatordriver.SetButtonLamp(elevatordriver.BT_HallUp, floor, btnPair[elevatorcontroller.HallUp])
		elevatordriver.SetButtonLamp(elevatordriver.BT_HallDown, floor, btnPair[elevatorcontroller.HallDown])
	}
}
