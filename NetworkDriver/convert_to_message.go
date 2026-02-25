package networkdriver

type ElevatorId string

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
	NodeID       string                   `json:"nodeID"`
	HallRequests [][2]RequestState        `json:"hallRequests"`
	Elevators    map[string]ElevatorState `json:"elevators"`
	Iter         int                      `json:"iter"`
}
