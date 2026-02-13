package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
	"os"
	"time"
)

// RunElevator runs the elevator controller event loop.
// It receives hardware events via the provided driver channels
// and communicates hall requests with the Manager.
//
// Driver channels (from elevio polling goroutines started externally):
//   - drv_buttons: button press events
//   - drv_floors:  floor sensor events
//   - drv_obstr:   obstruction switch events
//   - drv_stop:    stop button events
//
// Manager channels:
//   - hallButtonChan:  outbound hall button presses for Manager
//   - hallRequestChan: inbound assigned hall requests from Manager
func RunElevator(
	drv_buttons <-chan elevio.ButtonEvent,
	drv_floors <-chan int,
	drv_obstr <-chan bool,
	drv_stop <-chan bool,
	ButtonChan chan<- elevio.ButtonEvent,
	hallRequestChan <-chan [NumFloors][2]bool,
) {
	elevator := ElevatorUninitialized()
	timer := NewDoorTimer()

	if elevio.GetFloor() == -1 {
		FsmOnInitBetweenFloors(elevator)
	} else {
		elevator.Floor = elevio.GetFloor()
		elevio.SetFloorIndicator(elevator.Floor)
	}

	fmt.Println("Elevator controller started!")

	for {
		select {
		case btn := <-drv_buttons:
			fmt.Printf("Button event received: Floor=%d, Type=%v\n", btn.Floor, btn.Button)
			if btn.Button == elevio.BT_Cab {
				FsmOnRequestButtonPress(elevator, btn.Floor, btn.Button, timer)
			} else {

				ButtonChan <- btn
			}

		case floor := <-drv_floors:
			FsmOnFloorArrival(elevator, floor, timer)

		case <-timer.Chan():
			FsmOnDoorTimeout(elevator, timer)

			//TODO: might be inaccurate and not communicate well with the Network module
		case newHallRequests := <-hallRequestChan:
			fmt.Printf("DEBUG: Received hall requests from Manager\n")
			oldHallRequests := elevator.HallRequests
			elevator.HallRequests = newHallRequests
			setAllLights(elevator)

			if elevator.Behaviour == EB_Idle && hasNewRequests(oldHallRequests, newHallRequests) {
				fmt.Printf("DEBUG: Idle with new requests, choosing direction\n")
				pair := RequestsChooseDirection(elevator)
				elevator.Dirn = pair.Dirn
				elevator.Behaviour = pair.Behaviour
				fmt.Printf("DEBUG: Chose Dirn=%s, Behaviour=%s\n", dirnToString(elevator.Dirn), elevator.Behaviour.String())

				switch pair.Behaviour {
				case EB_DoorOpen:
					fmt.Printf("DEBUG: Opening doors at current floor\n")
					elevio.SetDoorOpenLamp(true)
					timer.Start(elevator.Config.DoorOpenDuration)
					RequestsClearAtCurrentFloor(elevator)
					setAllLights(elevator)
				case EB_Moving:
					fmt.Printf("DEBUG: Starting movement for hall requests\n")
					elevio.SetMotorDirection(elevator.Dirn)
				case EB_Idle:
					fmt.Printf("DEBUG: Staying idle\n")
				}
			} else {
				fmt.Printf("DEBUG: Not idle or no new requests\n")
			}

		case obstr := <-drv_obstr:
			fmt.Printf("Obstruction: %v\n", obstr)
			elevator.ObstructionActive = obstr
			if !obstr && elevator.Behaviour == EB_DoorOpen {
				timer.Start(elevator.Config.DoorOpenDuration)
			}

		case stop := <-drv_stop:

			fmt.Printf("Stop button pressed: %v\n", stop)

			// 1. Update Stop Lamp
			elevio.SetStopLamp(stop) // 'stop' is bool (active/inactive)

			if stop {
				// 2. Stop Motor Immediately
				elevio.SetMotorDirection(elevio.MD_Stop)

				// 3. Clear all requests (Emergency Reset)
				for f := 0; f < NumFloors; f++ {
					elevator.CabRequests[f] = false
					// Hall requests typically cleared too or re-assigned
				}
				setAllLights(elevator) // Turn off button lights

				// 4. Handle Door if at floor
				if elevator.Behaviour == EB_Moving {
					elevator.Dirn = elevio.MD_Stop
					elevator.Behaviour = EB_Idle
					// If between floors, we just stop and wait.
				} else if elevator.Behaviour == EB_Idle || elevator.Behaviour == EB_DoorOpen {
					// If at floor, force doors open
					elevio.SetDoorOpenLamp(true)
					timer.Start(elevator.Config.DoorOpenDuration)
					elevator.Behaviour = EB_DoorOpen
				}
				//TODO: let this be running a bash script that exits the program and terminates the sim mby?
				fmt.Println("Exiting application in 5 seconds...")
				time.Sleep(5 * time.Second)
				os.Exit(0)
			}
		}
	}
}

func hasNewRequests(old, new_ [NumFloors][2]bool) bool {
	for f := 0; f < NumFloors; f++ {
		if (new_[f][0] && !old[f][0]) || (new_[f][1] && !old[f][1]) {
			return true
		}
	}
	return false
}
