package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
	"os"
	"time"
)

func RunElevator(
	drv_buttons <-chan elevio.ButtonEvent,
	drv_floors <-chan int,
	drv_obstr <-chan bool,
	drv_stop <-chan bool,
	orderChan chan<- elevio.ButtonEvent,
	servedChan chan<- elevio.ButtonEvent,
	elevatorStateChan chan<- Elevator,
	hallRequestChan <-chan [][2]bool,
) {
	elevator := ElevatorUninitialized()
	timer := NewDoorTimer()

	if elevio.GetFloor() == -1 {
		FsmOnInitBetweenFloors(elevator)
	}

	broadcast := func() {
		select {
		case elevatorStateChan <- *elevator:
		default:
		}
	}

	forwardServed := func(served []elevio.ButtonEvent) {
		for _, btn := range served {
			servedChan <- btn
		}
	}

	fmt.Println("Elevator controller started!")

	for {
		select {
		case btn := <-drv_buttons:
			fmt.Printf("Button event: Floor=%d Type=%s\n", btn.Floor, ButtonToString(btn.Button))
			if btn.Button == elevio.BT_Cab {
				forwardServed(CabRequestButtonPress(elevator, btn.Floor, timer))
			} else {
				orderChan <- btn
			}

		case floor := <-drv_floors:
			forwardServed(FsmOnFloorArrival(elevator, floor, timer))

		case <-timer.Chan():
			forwardServed(FsmOnDoorTimeout(elevator, timer))

		case newHallRequests := <-hallRequestChan:
			fmt.Printf("DEBUG: Received hall requests from Manager\n")
			replaceHallRequests(elevator, newHallRequests)
			if elevator.Behaviour == EB_Idle {
				forwardServed(FsmActOnBehaviourPair(elevator, RequestsChooseDirection(elevator), timer))
			}

		case obstr := <-drv_obstr:
			fmt.Printf("Obstruction: %v\n", obstr)
			elevator.ObstructionActive = obstr
			if !obstr && elevator.Behaviour == EB_DoorOpen {
				timer.Start(elevator.Config.DoorOpenDuration)
			}

		case stop := <-drv_stop:
			fmt.Printf("Stop button: %v\n", stop)
			elevio.SetStopLamp(stop)
			if stop {
				elevio.SetMotorDirection(elevio.MD_Stop)
				for f := 0; f < NumFloors; f++ {
					elevator.CabRequests[f] = false
				}
				switch elevator.Behaviour {
				case EB_Moving:
					elevator.Direction = elevio.MD_Stop
					elevator.Behaviour = EB_Idle
				case EB_Idle, EB_DoorOpen:
					elevio.SetDoorOpenLamp(true)
					timer.Start(elevator.Config.DoorOpenDuration)
					elevator.Behaviour = EB_DoorOpen
				}
				fmt.Println("Exiting in 5 seconds...")
				time.Sleep(5 * time.Second)
				os.Exit(0)
			}
		}

		broadcast()
	}
}
