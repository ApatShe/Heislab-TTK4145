package networknode

import (
	elevatorcontroller "Heislab/elevatorcontroller"
	elevatordriver "Heislab/elevatordriver"
	"Heislab/node_communication/peers"
)

type RequestState uint8

const (
	UNKNOWN   RequestState = iota
	INACTIVE               // 1
	REQUESTED              // 2
	ACTIVE                 // 3
)

type ElevatorState struct {
	Behaviour      string         `json:"behaviour"`
	Floor          int            `json:"floor"`
	Direction      string         `json:"direction"`
	CabRequests    []RequestState `json:"cabRequests"`
	DoorOpen       bool           `json:"doorOpen"`
	IsOutOfService bool           `json:"isOutOfService"`
}

type NetworkSnapshot struct {
	NodeID       string                       `json:"nodeID"`
	HallRequests map[string][][2]RequestState `json:"hallRequests"`
	Elevators    map[string]ElevatorState     `json:"states"`
	Iter         uint64                       `json:"iter"`

	ReconnectedNode bool `json:"reconnectedNode"`
}

// HallUpIdx and HallDownIdx are the array indices for the two-element button
// axis in [][2]RequestState. They mirror elevatorcontroller.HallUp/HallDown.
const (
	HallUpIdx   = elevatorcontroller.HallUp   // = 0
	HallDownIdx = elevatorcontroller.HallDown // = 1
)

type NetworkNodeIn struct {
	CabButton      <-chan elevatordriver.ButtonEvent
	HallButton     <-chan elevatordriver.ButtonEvent
	ElevatorState  <-chan elevatorcontroller.Elevator
	ServedRequests <-chan elevatordriver.ButtonEvent
}

type NetworkNodeOut struct {
	Snapshot          chan<- NetworkSnapshot
	PeerUpdate        chan<- peers.PeerUpdate
	ElevatorInitState chan<- elevatorcontroller.ElevatorInitState
}

func LocalElevatorToElevatorState(elevator elevatorcontroller.Elevator, cabRequests []RequestState) ElevatorState {
	return ElevatorState{
		Behaviour:      elevator.Behaviour.String(),
		Floor:          elevator.Floor,
		Direction:      elevatorcontroller.DirnToString(elevator.Direction),
		CabRequests:    cabRequests,
		DoorOpen:       elevator.Behaviour == elevatorcontroller.EB_DoorOpen,
		IsOutOfService: elevator.IsOutOfService,
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
