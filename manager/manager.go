package manager

import (
	elevatorcontroller "Heislab/ElevatorController"
	log "Heislab/Log"
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
	CabRequests  chan<- []bool        // consensused cab requests → elevator FSM
	HallRequests chan<- [][2]bool     // HRA-assigned matrix → elevator
	Lights       chan<- RequestLights // HRA-assigned request matrix and active cabRequest → lights
	DoorInit     chan<- bool          // persistent door state → door module on first snapshot
}

type RequestLights struct {
	HallLights [][2]bool
	CabLights  []bool
}

func hallRequestToHRAInput(snapshot networkdriver.NetworkSnapshot) [][2]bool {
	hraInput := make([][2]bool, elevatorcontroller.NumFloors)
	for _, peerRequests := range snapshot.HallRequests {
		if peerRequests == nil {
			continue
		}
		for floor, btnPair := range peerRequests {
			if btnPair[networkdriver.HallUpIdx] == networkdriver.ACTIVE {
				hraInput[floor][elevatorcontroller.HallUp] = true
			}
			if btnPair[networkdriver.HallDownIdx] == networkdriver.ACTIVE {
				hraInput[floor][elevatorcontroller.HallDown] = true
			}
		}
	}
	return hraInput
}

func cabLightsFromSnapshot(snapshot networkdriver.NetworkSnapshot, id string) []bool {
	elevatorState, exists := snapshot.Elevators[id]
	if !exists {
		return nil
	}
	cabLights := make([]bool, len(elevatorState.CabRequests))
	for floor, requestState := range elevatorState.CabRequests {
		cabLights[floor] = networkdriver.RequestStateToBool(requestState)
	}
	return cabLights
}

// hallLightsFromSnapshot returns a lights matrix that is true for any button
// where at least one active peer has reached ACTIVE consensus. This ensures all
// simulators light the same buttons regardless of which node pressed them.
func hallLightsFromSnapshot(snapshot networkdriver.NetworkSnapshot, activeElevators map[string]bool) [][2]bool {
	lights := make([][2]bool, elevatorcontroller.NumFloors)
	for nodeID := range activeElevators {
		peerRequests := snapshot.HallRequests[nodeID]
		if peerRequests == nil {
			continue
		}
		for floor, btnPair := range peerRequests {
			if btnPair[networkdriver.HallUpIdx] == networkdriver.ACTIVE {
				lights[floor][elevatorcontroller.HallUp] = true
			}
			if btnPair[networkdriver.HallDownIdx] == networkdriver.ACTIVE {
				lights[floor][elevatorcontroller.HallDown] = true
			}
		}
	}
	return lights
}

func extractActiveElevatorStates(snapshot networkdriver.NetworkSnapshot, activeElevators map[string]bool) map[string]HRAElevState {
	elevatorStates := make(map[string]HRAElevState)
	for nodeID, elevatorState := range snapshot.Elevators {
		if !activeElevators[nodeID] {
			continue
		}
		if elevatorState.Floor == -1 {
			continue // not yet initialized, skip to avoid crashing HRA
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

func hallRequestsEqual(incoming, last [][2]bool) bool {
	if len(incoming) != len(last) {
		return false
	}
	for floor := range incoming {
		if incoming[floor] != last[floor] {
			return false
		}
	}
	return true
}

func cabRequestsEqual(incoming, last []bool) bool {
	if len(incoming) != len(last) {
		return false
	}
	for i := range incoming {
		if incoming[i] != last[i] {
			return false
		}
	}
	return true
}

func RunManager(in ManagerIn, out ManagerOut, id string) {
	activeElevators := map[string]bool{id: true} // always treat self as active
	//doorInitSent := false

	//var lastHallRequests [][2]bool
	//var lastHallLights [][2]bool

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

			log.Log("[Manager] Received snapshot iter=%d from node %s with %d elevators and hall requests: %v", snapshot.Iter, snapshot.NodeID, len(snapshot.Elevators), snapshot.HallRequests)
			// Always add any node present in the snapshot as active

			consensusCabRequests := snapshot.Elevators[id].CabRequests
			consensusCabRequestsBool := make([]bool, len(consensusCabRequests))
			for i, requestState := range consensusCabRequests {
				consensusCabRequestsBool[i] = networkdriver.RequestStateToBool(requestState)
			}
			log.Log("[Manager] Sending Consensus cab requests to FSM for self: %v", consensusCabRequestsBool)
			select {
			case out.CabRequests <- consensusCabRequestsBool:
			default:
			}

			consensusHallRequests := hallRequestToHRAInput(snapshot)

			hraInput := HRAInput{
				HallRequests: consensusHallRequests,
				States:       extractActiveElevatorStates(snapshot, activeElevators),
			}

			//if len(hraInput.States) == 0 {
			//	break
			//}

			delegatedHallRequests := OutputHallRequestAssigner(hraInput)
			designatedHallRequests := extractDesignatedHallRequests(delegatedHallRequests, id)
			if designatedHallRequests != nil {
				select {
				case out.HallRequests <- designatedHallRequests:
				default:
				}
			}

			hallLights := hallLightsFromSnapshot(snapshot, activeElevators)
			cabLights := cabLightsFromSnapshot(snapshot, id)

			Lights := RequestLights{
				HallLights: hallLights,
				CabLights:  cabLights,
			}

			select {
			case out.Lights <- Lights:
			default:
			}

		}
	}
}
