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

	// We iterate through all nodes' hall requests in the snapshot.
	// If ANY node has reached ACTIVE for a specific button, the HRA
	// should treat that as a live order to be assigned.
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

func RunManager(in ManagerIn, out ManagerOut, id string) {
	activeElevators := map[string]bool{id: true} // always treat self as active
	//doorInitSent := false

	var lastHallRequests [][2]bool
	var lastHallLights [][2]bool

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
			// Always add any node present in the snapshot as active
			for nodeID := range snapshot.Elevators {
				activeElevators[nodeID] = true
			}

			consensusHallRequests := hallRequestToHRAInput(snapshot)

			hallLights := hallLightsFromSnapshot(snapshot, activeElevators)
			if !hallRequestsEqual(hallLights, lastHallLights) {
				lastHallLights = hallLights
				select {
				case out.HallLights <- hallLights:
				default:
				}
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
			if designatedHallRequests != nil && !hallRequestsEqual(designatedHallRequests, lastHallRequests) {
				lastHallRequests = designatedHallRequests
				select {
				case out.HallRequests <- designatedHallRequests:
				default:
				}
			}
		}
	}
}
