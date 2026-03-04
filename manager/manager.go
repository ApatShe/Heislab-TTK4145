package manager

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/Network/network/peers"
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

func extractActiveElevatorStates(snapshot networkdriver.NetworkSnapshot, activeElevators map[string]bool) map[string]HRAElevState {
	elevatorStates := make(map[string]HRAElevState)
	for nodeID, elevatorState := range snapshot.Elevators {
		if !activeElevators[nodeID] {
			continue
		}
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

func extractDesignatedHallRequests(delegatedHallRequests map[string][][2]bool, id string) [][2]bool {
	if delegatedHallRequests == nil {
		return nil
	}
	return delegatedHallRequests[id]
}

func RunManager(
	snapshotChan <-chan networkdriver.NetworkSnapshot,
	peerUpdateToManagerChan <-chan peers.PeerUpdate,

	hallRequestChan chan<- [][2]bool,
	hallLightsChan chan<- [][2]bool,
	id string,
) {
	activeElevators := make(map[string]bool)

	for {
		select {
		case peerUpdate := <-peerUpdateToManagerChan:
			for _, lostID := range peerUpdate.Lost {
				delete(activeElevators, lostID)
			}
			for _, peerID := range peerUpdate.Peers {
				activeElevators[peerID] = true
			}

		case snapshot := <-snapshotChan:
			consensusHallRequests := hallRequestToHRAInput(snapshot)

			select {
			case hallLightsChan <- consensusHallRequests:
			default:
			}

			hraInput := HRAInput{
				HallRequests: consensusHallRequests,
				States:       extractActiveElevatorStates(snapshot, activeElevators),
			}

			delegatedHallRequests := OutputHallRequestAssigner(hraInput)
			designatedHallRequests := extractDesignatedHallRequests(delegatedHallRequests, id)
			if designatedHallRequests != nil {
				hallRequestChan <- designatedHallRequests
			}
		}
	}
}
