package networknode

import (
	log "Heislab/Log"
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

	// ReconnectedNode is true while the sending node is in its reconnect
	// cooldown window (set at self-lost, cleared after reconnectCooldownTicks).
	// Peers use this flag to:
	//   (a) skip propagateResetsToOwn — sender's INACTIVEs are stale offline serves
	//   (b) not let sender's INACTIVE overwrite their own ACTIVE view of the sender
	// The sending node uses its own copy of this flag to:
	//   (c) lift the isUnResettingState guard on its own entry so it can adopt
	//       the network's ACTIVE perspective of what it still owes.
	ReconnectedNode bool `json:"reconnectedNode"`
}

// HallUpIdx and HallDownIdx are the array indices for the two-element button
// axis in [][2]RequestState. They mirror elevatorcontroller.HallUp/HallDown.
const (
	HallUpIdx   = elevatorcontroller.HallUp   // = 0
	HallDownIdx = elevatorcontroller.HallDown // = 1
)

// NetworkNodeIn groups all channels that deliver events into RunNetworkNode.
type NetworkNodeIn struct {
	CabButton      <-chan elevatordriver.ButtonEvent  // local cab button presses
	HallButton     <-chan elevatordriver.ButtonEvent  // local hall button presses
	ElevatorState  <-chan elevatorcontroller.Elevator // local elevator FSM state
	ServedRequests <-chan elevatordriver.ButtonEvent  // served requests to clear
}

// NetworkNodeOut groups all channels that RunNetworkNode writes into.
type NetworkNodeOut struct {
	Snapshot          chan<- NetworkSnapshot  // consensus state → RunManager
	PeerUpdate        chan<- peers.PeerUpdate // peer list changes → RunManager
	ElevatorInitState chan<- elevatorcontroller.ElevatorInitState
}

func LocalElevatorToElevatorState(elevator elevatorcontroller.Elevator, cabRequests []RequestState) ElevatorState {
	log.Log("[CONVERSION] Making snapshot with IsOutOfService=%t", elevator.IsOutOfService)
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
