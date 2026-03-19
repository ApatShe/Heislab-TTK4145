package elevatorcontroller

import (
	log "Heislab/Log"
	elevatordriver "Heislab/elevatordriver"
	"time"
)

// RunElevator runs the elevator finite state machine.
//
// Two distinct order paths:
//   - in.CabButton  — cab button events arriving directly from hardware polling.
//   - in.HallRequests — full hall-request matrix pushed by the coordinator after
//     the HRA has run and the network has reached consensus.
//
// Hall button presses never arrive here directly; they travel:
//
//	button → RunNetworkNode → coordinator/HRA → network broadcast → consensus
//	→ coordinator pushes [][2]bool matrix → in.HallRequests → here.
func RunElevator(in ElevatorIn, out ElevatorOut) {
	// Block until NetworkNode signals that peer state has been recovered.
	// This prevents the motor from starting before cab requests are known.
	initState := <-in.ElevatorInitState
	log.Log("[INIT] elevator uninitialized, recovered/initial cab requests: %v", initState.CabRequests)

	var cabArray [NumFloors]bool
	for i, v := range initState.CabRequests {
		if i < NumFloors {
			cabArray[i] = v
		}
	}

	elevator := ElevatorUninitialized(cabArray)

	if elevatordriver.GetFloor() == -1 {
		elevatordriver.SetMotorDirection(elevatordriver.MD_Down)
		elevator.Direction = elevatordriver.MD_Down
		elevator.Behaviour = EB_Moving
		out.ResetMotorTimer <- struct{}{}
		log.Log("[elevator] between floors on startup — moving down to find floor")
	} else if initState.DoorOpen {
		elevator.Behaviour = EB_DoorOpen

		select {
		case out.DoorOpen <- struct{}{}:
		default:
		}
		log.Log("[elevator] restoring door-open state from peer snapshot")
	} else {
		select {
		case out.ConfirmDoorClosed <- struct{}{}:
		default:
		}
		log.Log("[elevator] no door state to recover — confirming door closed")
	}

	broadcastState := func() {
		if elevator.Floor == -1 {
			select {
			case out.LightsState <- *elevator:
			default:
			}
			return
		}
		select {
		case out.NetworkState <- *elevator:
		default:
		}
		select {
		case out.LightsState <- *elevator:
		default:
		}
	}

	reportServedRequests := func(served []elevatordriver.ButtonEvent) {
		for _, btn := range served {
			log.Log("[SERVED] reporting served request: floor=%d btn=%d", btn.Floor, btn.Button)
			out.ServedRequests <- btn
		}
	}

	// log.Log("Elevator controller started!")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastFloor := elevator.Floor
	for {
		var served []elevatordriver.ButtonEvent
		var commands []ElevatorCommand

		select {

		case newCabRequest := <-in.CabRequests:
			log.Log("[elevator] cab request update: %v", newCabRequest)
			served, commands = FsmOnCabRequests(elevator, newCabRequest)
			log.Log("[elevator] served: %v", served)

		case newHallRequests := <-in.HallRequests:
			// log.Log("Hall request update from coordinator\n")
			served, commands = FsmOnHallRequestsUpdate(elevator, newHallRequests)

		case floor := <-in.Floor:
			served, commands = FsmOnFloorArrival(elevator, floor)

		case floor := <-in.Floor:
			served, commands = FsmOnFloorArrival(elevator, floor)

			// If floor advanced, that's evidence of motion → reset watchdog and clear OOS.
			if elevator.Floor != lastFloor {
				lastFloor = elevator.Floor
				select {
				case out.ResetMotorTimer <- struct{}{}:
				default:
				}
				if elevator.IsOutOfService {
					log.Log("[elevator] recovered: floor changed → clearing IsOutOfService")
					elevator.IsOutOfService = false
				}
			}

		case <-in.DoorClosed:
			served, commands = FsmOnDoorClose(elevator)

		case <-in.MotorStall:
			if elevator.Behaviour == EB_Moving && elevator.Direction != elevatordriver.MD_Stop {
				log.Log("[elevator] motor stall detected! Degrading gracefully.")
				log.Log("[elevator] OUT of service status WAS: %t", elevator.IsOutOfService)
			}

			elevator.Direction = elevatordriver.MD_Stop
			elevator.Behaviour = EB_Idle
			elevator.IsOutOfService = true
			elevatordriver.SetMotorDirection(elevatordriver.MD_Stop)
			select {
			case out.StopMotorTimer <- struct{}{}:
			default:
			}
		}

		reportServedRequests(served)
		executeCommands(commands, out)
		broadcastState()
	}
}

func executeCommands(commands []ElevatorCommand, out ElevatorOut) {
	for _, cmd := range commands {
		cmd.execute(out)
	}
}
