package networkdriver

import (
	elevatorcontroller "Heislab/ElevatorController"
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
}

type NetworkSnapshot struct {
	NodeID       string                       `json:"nodeID"`
	HallRequests map[string][][2]RequestState `json:"hallRequests"`
	Elevators    map[string]ElevatorState     `json:"states"`
	Iter         uint64                       `json:"iter"`
}

func LocalElevatorToElevatorState(elevator elevatorcontroller.Elevator) ElevatorState {
	cabs := make([]RequestState, len(elevator.CabRequests))
	for i, req := range elevator.CabRequests {
		cabs[i] = boolToRequestState(req)
	}
	return ElevatorState{
		Behaviour:   elevator.Behaviour.String(),
		Floor:       elevator.Floor,
		Direction:   elevator.Direction.String(),
		CabRequests: cabs,
	}
}

func HallRequestsToRequestStates(halls [][]bool) [][2]RequestState {
	result := make([][2]RequestState, len(halls))
	for i, floor := range halls {
		result[i] = [2]RequestState{
			boolToRequestState(floor[0]),
			boolToRequestState(floor[1]),
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
