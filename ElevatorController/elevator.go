package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
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

func (eb ElevatorBehaviour) String() string {
	switch eb {
	case EB_Idle:
		return "idle"
	case EB_DoorOpen:
		return "doorOpen"
	case EB_Moving:
		return "moving"
	default:
		return "unknown"
	}
}

// Config holds elevator configuration parameters
type Config struct {
	//TODO: a lot more to do here given the init bash file
	DoorOpenDuration time.Duration
}

// Elevator represents the state of a single elevator
type Elevator struct {
	Behaviour   ElevatorBehaviour
	Floor       int
	Direction   elevio.MotorDirection
	CabRequests [NumFloors]bool

	Config Config
}

// DirnBehaviourPair pairs a direction with a behaviour
type DirnBehaviourPair struct {
	Dirn      elevio.MotorDirection
	Behaviour ElevatorBehaviour
}

// ElevatorUninitialized returns a new elevator in the uninitialized state.
func ElevatorUninitialized() *Elevator {
	return &Elevator{
		Floor:       -1,
		Direction:   elevio.MD_Stop,
		CabRequests: [NumFloors]bool{},
		Behaviour:   EB_Idle,
		Config: Config{
			DoorOpenDuration: 3 * time.Second,
		},
	}
}

// ElevatorPrint prints the current elevator state (for debugging)
func ElevatorPrint(elevator *Elevator) {
	fmt.Printf("  +----+-----+---+----------+\n")
	fmt.Printf("  |%-4s| ^ v | C |%-10s|\n", "Flr", "Behaviour")
	fmt.Printf("  +----+-----+---+----------+\n")
	for f := NumFloors - 1; f >= 0; f-- {
		floorMarker := " "
		if elevator.Floor == f {
			floorMarker = "*"
		}
		hUp := "-"
		if //elevator.HallRequests[f][0] {
			hUp = "^"
		}
		hDn := "-"
		if //elevator.HallRequests[f][1] {
			hDn = "v"
		}
		cab := "-"
		if elevator.CabRequests[f] {
			cab = "C"
		}
		if f == elevator.Floor {
			fmt.Printf("%s |%-4d| %s %s | %s |%-10s|\n", floorMarker, f, hUp, hDn, cab, elevator.Behaviour.String())
		} else {
			fmt.Printf("%s |%-4d| %s %s | %s |          |\n", floorMarker, f, hUp, hDn, cab)
		}
	}
	fmt.Printf("  +----+-----+---+----------+\n")
	fmt.Printf("  Direction: %s\n", dirnToString(elevator.Direction))
}

// DirectionToString converts a MotorDirection to a string
func dirnToString(d elevio.MotorDirection) string {
	switch d {
	case elevio.MD_Up:
		return "up"
	case elevio.MD_Down:
		return "down"
	case elevio.MD_Stop:
		return "stop"
	default:
		return "stop"
	}
}

// ButtonToString converts a ButtonType to a string
func ButtonToString(b elevio.ButtonType) string {
	switch b {
	case elevio.BT_HallUp:
		return "HallUp"
	case elevio.BT_HallDown:
		return "HallDown"
	case elevio.BT_Cab:
		return "Cab"
	default:
		return "Unknown"
	}
}
