package elevatorcontroller

import (
	elevatordriver "Heislab/elevatordriver"
	"time"
)

func RunElevator(in ElevatorIn, out ElevatorOut) {
	initState := <-in.ElevatorInitState

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
	} else if initState.DoorOpen {
		elevator.Behaviour = EB_DoorOpen

		select {
		case out.DoorOpen <- struct{}{}:
		default:
		}
	} else {
		select {
		case out.ConfirmDoorClosed <- struct{}{}:
		default:
		}
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
			out.ServedRequests <- btn
		}
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastFloor := elevator.Floor
	for {
		var served []elevatordriver.ButtonEvent
		var commands []ElevatorCommand

		select {

		case newCabRequest := <-in.CabRequests:
			served, commands = FsmOnCabRequests(elevator, newCabRequest)

		case newHallRequests := <-in.HallRequests:
			served, commands = FsmOnHallRequestsUpdate(elevator, newHallRequests)

		case floor := <-in.Floor:
			served, commands = FsmOnFloorArrival(elevator, floor)

		case floor := <-in.Floor:
			served, commands = FsmOnFloorArrival(elevator, floor)

			if elevator.Floor != lastFloor {
				lastFloor = elevator.Floor
				select {
				case out.ResetMotorTimer <- struct{}{}:
				default:
				}
				if elevator.IsOutOfService {
					elevator.IsOutOfService = false
				}
			}

		case <-in.DoorClosed:
			served, commands = FsmOnDoorClose(elevator)

		case <-in.MotorStall:
			if elevator.Behaviour == EB_Moving && elevator.Direction != elevatordriver.MD_Stop {
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
