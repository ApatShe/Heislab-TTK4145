package elevatorcontroller

import (
	log "Heislab/Log"
	"Heislab/driver-go/elevio"
)

// RunElevator runs the elevator finite state machine.
//
// Two distinct order paths:
//   - in.CabButton  — cab button events arriving directly from hardware polling.
//   - in.HallRequests — full hall-request matrix pushed by the manager after
//     the HRA has run and the network has reached consensus.
//
// Hall button presses never arrive here directly; they travel:
//
//	button → RunNetworkNode → manager/HRA → network broadcast → consensus
//	→ manager pushes [][2]bool matrix → in.HallRequests → here.
func RunElevator(in ElevatorIn, out ElevatorOut) {
	// Block until NetworkNode signals that peer state has been recovered.
	// This prevents the motor from starting before cab requests are known.
	<-in.Init

	elevator := ElevatorUninitialized()
	if elevio.GetFloor() == -1 {
		elevio.SetMotorDirection(elevio.MD_Down)
		elevator.Direction = elevio.MD_Down
		elevator.Behaviour = EB_Moving
		out.ResetMotorTimer <- struct{}{}
	}

	broadcastState := func() {
		select {
		case out.NetworkState <- *elevator:
		default:
		}
		select {
		case out.LightsState <- *elevator:
		default:
		}
	}

	reportServedHallRequests := func(served []elevio.ButtonEvent) {
		for _, btn := range served {
			isHallButton := btn.Button == elevio.BT_HallUp || btn.Button == elevio.BT_HallDown
			if isHallButton {
				out.ServedHall <- btn // blocking: this MUST reach the manager
			}
		}
	}

	log.Log("Elevator controller started!")

	for {
		var served []elevio.ButtonEvent
		var commands []ElevatorCommand

		select {
		case btn := <-in.CabButton:
			log.Log("Cab button: Floor=%d\n", btn.Floor)
			served, commands = FsmOnCabRequest(elevator, btn.Floor)

		case newHallRequests := <-in.HallRequests:
			log.Log("Hall request update from manager\n")
			served, commands = FsmOnHallRequestsUpdate(elevator, newHallRequests)

		case floor := <-in.Floor:
			served, commands = FsmOnFloorArrival(elevator, floor)

		case <-in.DoorClosed:
			served, commands = FsmOnDoorClose(elevator)

		case <-in.MotorStall:
			// Motor stall detected — stop motor and return to idle.
			// The HRA will re-assign requests once the elevator broadcasts
			// its new idle state.
			log.Log("Motor watchdog: stall detected, stopping motor")
			elevator.Direction = elevio.MD_Stop
			elevator.Behaviour = EB_Idle
			elevio.SetMotorDirection(elevio.MD_Stop)
			select {
			case out.StopMotorTimer <- struct{}{}:
			default:
			}
		}

		reportServedHallRequests(served)
		executeCommands(commands, out)
		broadcastState()
	}
}

func executeCommands(commands []ElevatorCommand, out ElevatorOut) {
	for _, cmd := range commands {
		cmd.execute(out)
	}
}
