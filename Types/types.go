package types

// WorldView represents an elevator's state shared across the network
type WorldView struct {
	Behaviour   string `json:"behaviour"`
	Floor       int    `json:"floor"`
	Direction   string `json:"direction"`
	CabRequests []bool `json:"cabRequests"`
}

// CostFunctionInput is what Manager sends to the cost function
type CostFunctionInput struct {
	HallRequests [][2]bool            `json:"hallRequests"`
	States       map[string]WorldView `json:"states"` // "one", "two", etc.
}
