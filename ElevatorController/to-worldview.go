package elevatorcontroller

import types "Heislab/Types"

// ToWorldView converts the elevator state to shareable format
func (e *Elevator) ElevatorStatesToWorldView() types.WorldView {
	return types.WorldView{
		Behaviour:   e.Behaviour.String(),
		Floor:       e.Floor,
		Direction:   dirnToString(e.Dirn),
		CabRequests: e.CabRequests[:],
	}
}
