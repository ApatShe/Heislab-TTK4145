package elevatorcontroller

import types "Heislab/Types"

// ToWorldView converts the elevator state to shareable format
func (e *Elevator) ElevatorStatesToWorldView() types.NetworkSnapshot {
	return types.NetworkSnapshot{
		Behaviour:   e.Behaviour.String(),
		Floor:       e.Floor,
		Direction:   dirnToString(e.Direction),
		CabRequests: e.CabRequests[:],
	}
}
