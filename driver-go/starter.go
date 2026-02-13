package driver

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/driver-go/elevio"
)

// StartElevator initializes the hardware driver, creates all necessary channels,
// starts polling goroutines, and launches the elevator controller.
// This abstracts away all the boilerplate setup from main.
//
// Returns the manager communication channels:
//   - hallButtonChan: receive hall button presses from elevator
//   - hallRequestChan: send assigned hall requests to elevator
func StartElevator(addr string, numFloors int) (
	hallButtonChan <-chan elevio.ButtonEvent,
	hallRequestChan chan<- [elevatorcontroller.NumFloors][2]bool,
) {
	// Initialize hardware
	elevio.Init(addr, numFloors)

	// Create driver channels
	drv_buttons := make(chan elevio.ButtonEvent)
	drv_floors := make(chan int)
	drv_obstr := make(chan bool)
	drv_stop := make(chan bool)

	// Start hardware polling goroutines
	go elevio.PollButtons(drv_buttons)
	go elevio.PollFloorSensor(drv_floors)
	go elevio.PollObstructionSwitch(drv_obstr)
	go elevio.PollStopButton(drv_stop)

	// Create manager communication channels
	hallBtnChan := make(chan elevio.ButtonEvent)
	hallReqChan := make(chan [elevatorcontroller.NumFloors][2]bool)

	// Start elevator controller
	go elevatorcontroller.RunElevator(
		drv_buttons,
		drv_floors,
		drv_obstr,
		drv_stop,
		hallBtnChan,
		hallReqChan,
	)

	return hallBtnChan, hallReqChan
}
