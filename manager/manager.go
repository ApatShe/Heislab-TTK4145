package manager

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/Network/network/peers"
	networkdriver "Heislab/NetworkDriver"
)

// ManagerIn groups all channels that deliver events into RunManager.
type ManagerIn struct {
	Snapshot   <-chan networkdriver.NetworkSnapshot // consensus snapshot from network node
	PeerUpdate <-chan peers.PeerUpdate              // peer list changes from network node
}

// ManagerOut groups all channels that RunManager writes into.
type ManagerOut struct {
	HallRequests chan<- [][2]bool // HRA-assigned matrix → elevator
	HallLights   chan<- [][2]bool // HRA-assigned matrix → lights
	DoorInit     chan<- bool      // persistent door state → door module on first snapshot
}

func hallRequestToHRAInput(snapshot networkdriver.NetworkSnapshot) [][2]bool {
	hraInput := make([][2]bool, elevatorcontroller.NumFloors)
	// HallRequests is map[nodeID][][2]RequestState — iterate over node entries,
	// not floor indices. OR together all nodes' views: a request is active if
	// any node has it as ACTIVE.
	for _, nodeRequests := range snapshot.HallRequests {
		for floor, btnPair := range nodeRequests {
			hraInput[floor][elevatorcontroller.HallUp] = hraInput[floor][elevatorcontroller.HallUp] || networkdriver.RequestStateToBool(btnPair[networkdriver.HallUpIdx])
			hraInput[floor][elevatorcontroller.HallDown] = hraInput[floor][elevatorcontroller.HallDown] || networkdriver.RequestStateToBool(btnPair[networkdriver.HallDownIdx])
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

func RunManager(in ManagerIn, out ManagerOut, id string) {
	activeElevators := map[string]bool{id: true} // always treat self as active
	doorInitSent := false

	for {
		select {
		case peerUpdate := <-in.PeerUpdate:
			for _, lostID := range peerUpdate.Lost {
				delete(activeElevators, lostID)
			}
			for _, peerID := range peerUpdate.Peers {
				activeElevators[peerID] = true
			}

		case snapshot := <-in.Snapshot:
			// On the very first snapshot, restore door state to the door module.
			if !doorInitSent {
				doorInitSent = true
				if ownState, ok := snapshot.Elevators[id]; ok {
					select {
					case out.DoorInit <- ownState.DoorOpen:
					default:
					}
				}
			}

			consensusHallRequests := hallRequestToHRAInput(snapshot)

			select {
			case out.HallLights <- consensusHallRequests:
			default:
			}

			hraInput := HRAInput{
				HallRequests: consensusHallRequests,
				States:       extractActiveElevatorStates(snapshot, activeElevators),
			}

			if len(hraInput.States) == 0 {
				break
			}

			delegatedHallRequests := OutputHallRequestAssigner(hraInput)
			designatedHallRequests := extractDesignatedHallRequests(delegatedHallRequests, id)
			if designatedHallRequests != nil {
				select {
				case out.HallRequests <- designatedHallRequests:
				default:
				}
			}
		}
	}
}
