package manager

import (
	elevatorcontroller "Heislab/ElevatorController"
	networkdriver "Heislab/NetworkDriver"
	"Heislab/driver-go/elevio"
	"fmt"
)

func hallRequestToHRAInput(snapshot networkdriver.NetworkSnapshot) [][2]bool {
	hraInput := make([][2]bool, len(snapshot.HallRequests))
	for floor := range snapshot.HallRequests {
		for btn := range snapshot.HallRequests[floor] {
			hraInput[floor][btn] = networkdriver.RequestStateToBool(snapshot.HallRequests[floor][btn])
		}
	}
	return hraInput
}

func extractElevatorStates(snapshot networkdriver.NetworkSnapshot) map[string]HRAElevState {

	elevatorStates := make(map[string]HRAElevState, len(snapshot.Elevators))

	for nodeID, elevatorState := range snapshot.Elevators {
		cabRequests := make([]bool, len(elevatorState.CabRequests))

		for floor, requestState := range elevatorState.CabRequests {
			cabRequests[floor] = networkdriver.RequestStateToBool(requestState)
		}

		elevatorStates[nodeID] = HRAElevState{
			Behaviour:   elevatorState.Behaviour,
			Floor:       elevatorState.Floor,
			Direction:   elevatorState.Direction,
			CabRequests: cabRequests,
		}
	}
	return elevatorStates
}

func snapshotToHraInput(snapshot networkdriver.NetworkSnapshot) HRAInput {
	return HRAInput{
		HallRequests: hallRequestToHRAInput(snapshot),
		States:       extractElevatorStates(snapshot),
	}
}

func extractDesignatedHallRequests(delegatedHallRequests map[string][][2]bool, id string) [][2]bool {
	if delegatedHallRequests == nil {
		return nil
	}
	return delegatedHallRequests[id]
}

func RunManager(
	snapshotChan <-chan networkdriver.NetworkSnapshot,
	orderChan <-chan elevio.ButtonEvent,
	elevStateChan <-chan elevatorcontroller.Elevator,
	hallRequestChan chan<- [][2]bool,
	hallRequestStateChan chan<- [][2]networkdriver.RequestState,
	id string,
) {
	localHallRequests := make([][2]networkdriver.RequestState, elevatorcontroller.NumFloors)
	for {
		select {
		case button := <-orderChan:
			fmt.Printf("Manager Intercepted order: Floor=%d, Type=%v\n", button.Floor, button.Button)
			localHallRequests[button.Floor][int(button.Button)] = networkdriver.REQUESTED
			hallRequestStateChan <- localHallRequests

		case snapshot := <-snapshotChan:

			hraInput := snapshotToHraInput(snapshot)
			delegatedHallRequests := OutputHallRequesstAssigner(hraInput)

			designatedHallRequests := extractDesignatedHallRequests(delegatedHallRequests, id)

			hallRequestChan <- designatedHallRequests
		}
	}
}
