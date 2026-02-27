package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"time"
)

// Constants matching the C implementation
const (
	//TODO: change this to something configurable with the init bash file
	NumFloors = 4
)

// ElevatorBehaviour represents the elevator's current FSM state
type ElevatorBehaviour int

const (
	EB_Idle ElevatorBehaviour = iota
	EB_DoorOpen
	EB_Moving
)

// Config holds elevator configuration parameters
type Config struct {
	//TODO: a lot more to do here given the init bash file
	DoorOpenDuration time.Duration
}

// Elevator represents the state of a single elevator
type Elevator struct {
	Behaviour    ElevatorBehaviour
	Floor        int
	Direction    Direction
	CabRequests  [NumFloors]bool
	HallRequests [NumFloors][2]bool // [floor][0=up/1=down]

	Config Config
}

// ElevatorUninitialized returns a new elevator in the uninitialized state.
func ElevatorUninitialized() *Elevator {
	return &Elevator{
		Floor:        -1,
		Direction:    Direction{elevio.MD_Stop},
		CabRequests:  [NumFloors]bool{},
		HallRequests: [NumFloors][2]bool{},
		Behaviour:    EB_Idle,
		Config: Config{
			DoorOpenDuration: 3 * time.Second,
		},
	}
}
