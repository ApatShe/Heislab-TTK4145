package networkdriver

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/Network/network/peers"
	"Heislab/driver-go/elevio"
)

type RequestState uint8

const (
	UNKNOWN RequestState = iota
	INACTIVE
	REQUESTED
	ACTIVE
)

type ElevatorState struct {
	Behaviour   string         `json:"behaviour"`
	Floor       int            `json:"floor"`
	Direction   string         `json:"direction"`
	CabRequests []RequestState `json:"cabRequests"`
	DoorOpen    bool           `json:"doorOpen"`
}

type NetworkSnapshot struct {
	NodeID       string                       `json:"nodeID"`
	HallRequests map[string][][2]RequestState `json:"hallRequests"`
	Elevators    map[string]ElevatorState     `json:"states"`
	Iter         uint64                       `json:"iter"`
}

// HallUpIdx and HallDownIdx are the array indices for the two-element button
// axis in [][2]RequestState. They mirror elevatorcontroller.HallUp/HallDown.
const (
	HallUpIdx   = elevatorcontroller.HallUp   // = 0
	HallDownIdx = elevatorcontroller.HallDown // = 1
)

// NetworkNodeIn groups all channels that deliver events into RunNetworkNode.
type NetworkNodeIn struct {
	CabButton      <-chan elevio.ButtonEvent          // local cab button presses
	HallButton     <-chan elevio.ButtonEvent          // local hall button presses
	ElevatorState  <-chan elevatorcontroller.Elevator // local elevator FSM state
	ServedRequests <-chan elevio.ButtonEvent          // served requests to clear
}

// NetworkNodeOut groups all channels that RunNetworkNode writes into.
type NetworkNodeOut struct {
	Snapshot        chan<- NetworkSnapshot  // consensus state → RunManager
	PeerUpdate      chan<- peers.PeerUpdate // peer list changes → RunManager
	InitCabRequests chan<- []bool           // safe-to-start signal → RunElevator
}

func LocalElevatorToElevatorState(elevator elevatorcontroller.Elevator, cabRequests []RequestState) ElevatorState {

	return ElevatorState{
		Behaviour:   elevator.Behaviour.String(),
		Floor:       elevator.Floor,
		Direction:   elevatorcontroller.DirnToString(elevator.Direction),
		CabRequests: cabRequests,
		DoorOpen:    elevator.Behaviour == elevatorcontroller.EB_DoorOpen,
	}
}

func HallRequestsToRequestStates(halls [][]bool) [][2]RequestState {
	result := make([][2]RequestState, len(halls))
	for i, floor := range halls {
		result[i] = [2]RequestState{
			HallUpIdx:   boolToRequestState(floor[HallUpIdx]),
			HallDownIdx: boolToRequestState(floor[HallDownIdx]),
		}
	}
	return result
}

func RequestStateToBool(state RequestState) bool {
	return state == ACTIVE
}

func boolToRequestState(b bool) RequestState {
	if b {
		return ACTIVE
	}
	return INACTIVE
}
