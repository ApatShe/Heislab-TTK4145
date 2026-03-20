package coordinator

import (
	elevatorcontroller "Heislab/elevatorcontroller"
	networknode "Heislab/networknode"
	"Heislab/node_communication/peers"
)

type ManagerIn struct {
	Snapshot   <-chan networknode.NetworkSnapshot
	PeerUpdate <-chan peers.PeerUpdate
}

type ManagerOut struct {
	CabRequests  chan<- []bool
	HallRequests chan<- [][2]bool
	Lights       chan<- RequestLights
}

type RequestLights struct {
	HallLights [][2]bool
	CabLights  []bool
}

func hallRequestToHRAInput(snapshot networknode.NetworkSnapshot) [][2]bool {
	hraInput := make([][2]bool, elevatorcontroller.NumFloors)
	for _, peerRequests := range snapshot.HallRequests {
		if peerRequests == nil {
			continue
		}
		for floor, btnPair := range peerRequests {
			if btnPair[networknode.HallUpIdx] == networknode.ACTIVE {
				hraInput[floor][elevatorcontroller.HallUp] = true
			}
			if btnPair[networknode.HallDownIdx] == networknode.ACTIVE {
				hraInput[floor][elevatorcontroller.HallDown] = true
			}
		}
	}
	return hraInput
}

func cabLightsFromSnapshot(snapshot networknode.NetworkSnapshot, id string) []bool {
	elevatorState, exists := snapshot.Elevators[id]
	if !exists {
		return nil
	}
	cabLights := make([]bool, len(elevatorState.CabRequests))
	for floor, requestState := range elevatorState.CabRequests {
		cabLights[floor] = networknode.RequestStateToBool(requestState)
	}
	return cabLights
}
func hallLightsFromSnapshot(snapshot networknode.NetworkSnapshot, activeElevators map[string]bool) [][2]bool {
	lights := make([][2]bool, elevatorcontroller.NumFloors)
	for nodeID := range activeElevators {
		peerRequests := snapshot.HallRequests[nodeID]
		if peerRequests == nil {
			continue
		}
		for floor, btnPair := range peerRequests {
			if btnPair[networknode.HallUpIdx] == networknode.ACTIVE {
				lights[floor][elevatorcontroller.HallUp] = true
			}
			if btnPair[networknode.HallDownIdx] == networknode.ACTIVE {
				lights[floor][elevatorcontroller.HallDown] = true
			}
		}
	}
	return lights
}

func extractActiveElevatorStates(snapshot networknode.NetworkSnapshot, activeElevators map[string]bool) map[string]HRAElevState {
	elevatorStates := make(map[string]HRAElevState)
	for nodeID, elevatorState := range snapshot.Elevators {
		if !activeElevators[nodeID] {
			continue
		}

		if elevatorState.Floor == -1 {
			continue
		}
		if elevatorState.IsOutOfService {
			continue
		}

		cabRequests := make([]bool, len(elevatorState.CabRequests))
		for floor, requestState := range elevatorState.CabRequests {
			cabRequests[floor] = networknode.RequestStateToBool(requestState)
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
	activeElevators := map[string]bool{id: true}

	for {
		select {
		case peerUpdate := <-in.PeerUpdate:
			for _, lostID := range peerUpdate.Lost {
				if lostID != id {
					delete(activeElevators, lostID)
				}
			}
			for _, peerID := range peerUpdate.Peers {
				activeElevators[peerID] = true
			}

		case snapshot := <-in.Snapshot:

			consensusCabRequests := snapshot.Elevators[id].CabRequests
			consensusCabRequestsBool := make([]bool, len(consensusCabRequests))
			for i, requestState := range consensusCabRequests {
				consensusCabRequestsBool[i] = networknode.RequestStateToBool(requestState)
			}
			select {
			case out.CabRequests <- consensusCabRequestsBool:
			default:
			}

			consensusHallRequests := hallRequestToHRAInput(snapshot)

			hraInput := HRAInput{
				HallRequests: consensusHallRequests,
				States:       extractActiveElevatorStates(snapshot, activeElevators),
			}

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
