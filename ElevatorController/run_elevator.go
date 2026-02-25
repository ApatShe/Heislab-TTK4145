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
//   - ButtonChan:      outbound button events (hall + cab) for Manager
//   - hallRequestChan: inbound assigned hall requests from Manager
//
// Network channels:
//   - stateChan: outbound state snapshots for the network transmitter.
//     Sends are non-blocking; if the transmitter is not ready the snapshot
//     is dropped rather than stalling the FSM.
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
			hasNew := ReplaceHallRequests(newHallRequests) //TODO
			setAllLights(elevator)

			if elevator.Behaviour == EB_Idle && hasNew {
				fmt.Printf("DEBUG: Idle with new requests, choosing direction\n")
				elevator.Direction, elevator.Behaviour = RequestsChooseDirection()
				fmt.Printf("DEBUG: Chose Dirn=%s, Behaviour=%s\n", dirnToString(elevator.Direction), elevator.Behaviour.String())

				switch elevator.Behaviour {
				case EB_DoorOpen:
					fmt.Printf("DEBUG: Opening doors at current floor\n")
					elevio.SetDoorOpenLamp(true)
					timer.Start(elevator.Config.DoorOpenDuration)
					RequestsClearAtCurrentFloor(elevator)
					setAllLights(elevator)
				case EB_Moving:
					fmt.Printf("DEBUG: Starting movement for hall requests\n")
					elevio.SetMotorDirection(elevator.Direction)
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
			elevio.SetStopLamp(stop)

			if stop {
				// 2. Stop Motor Immediately
				elevio.SetMotorDirection(elevio.MD_Stop)

				// 3. Clear all requests (Emergency Reset)
				ClearAllCabRequests(elevator)
				// Hall requests typically cleared too or re-assigned
				setAllLights(elevator)

				// 4. Handle Door if at floor
				if elevator.Behaviour == EB_Moving {
					elevator.Direction = elevio.MD_Stop
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
