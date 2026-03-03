package manager

import (
	elevatorcontroller "Heislab/ElevatorController"
	networkdriver "Heislab/NetworkDriver"
)

func hallRequestToHRAInput(snapshot networkdriver.NetworkSnapshot) [][2]bool {
	hraInput := make([][2]bool, elevatorcontroller.NumFloors)
	// HallRequests is map[nodeID][][2]RequestState — iterate over node entries,
	// not floor indices. OR together all nodes' views: a request is active if
	// any node has it as ACTIVE.
	for _, nodeRequests := range snapshot.HallRequests {
		for floor, btnPair := range nodeRequests {
			hraInput[floor][0] = hraInput[floor][0] || networkdriver.RequestStateToBool(btnPair[0])
			hraInput[floor][1] = hraInput[floor][1] || networkdriver.RequestStateToBool(btnPair[1])
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
	hallRequestChan chan<- [][2]bool,
	hallLightsChan chan<- [][2]bool,
	id string,
) {
	for {
		snapshot := <-snapshotChan

		hraInput := snapshotToHraInput(snapshot)

		consensusHallRequests := hraInput.HallRequests

		//non-blocking send of consensus hall requests to the lights manager, so that we can still delegate to the HRA
		select {
		case hallLightsChan <- consensusHallRequests:
		default:
		}

		delegatedHallRequests := OutputHallRequesstAssigner(hraInput)
		designatedHallRequests := extractDesignatedHallRequests(delegatedHallRequests, id)
		if designatedHallRequests != nil {
			hallRequestChan <- designatedHallRequests
		}
	}
}
