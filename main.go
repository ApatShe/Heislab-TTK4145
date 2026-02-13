package main

import (
	elevatorcontroller "Heislab/ElevatorController"
	driver "Heislab/driver-go"
	"fmt"
	"time"
)

func main() {
	// Start elevator controller
	_, hallRequestChan := driver.StartElevator("localhost:15657", 4)

	// Inject temporary hall request for testing
	go func() {
		// Wait a moment for startup
		time.Sleep(2 * time.Second)

		fmt.Println("Injecting Hall Request: Floor 3 Down")
		requests := [elevatorcontroller.NumFloors][2]bool{}
		requests[3][1] = true // Floor 3 Down

		hallRequestChan <- requests
	}()

	select {}
}
