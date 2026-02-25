package types

const (
	UNKNOWN uint8 = iota
	INACTIVE
	REQUESTED
	ACTIVE
)

// NetworkSnapshot represents an elevator's state shared across the network
type NetworkSnapshot struct {
	Behaviour   string `json:"behaviour"`
	Floor       int    `json:"floor"`
	Direction   string `json:"direction"`
	CabRequests []bool `json:"cabRequests"`
}

// CostFunctionInput is what Manager sends to the cost function
type CostFunctionInput struct {
	HallRequests [][2]bool                  `json:"hallRequests"`
	States       map[string]NetworkSnapshot `json:"states"` // "one", "two", etc.
}
