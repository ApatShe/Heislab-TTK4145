package elevatorcontroller

import (
	elevatordriver "Heislab/elevatordriver"
	"fmt"
	"time"
)

const NumFloors = 4

// HallUp and HallDown are the array indices for the two-element hall-request
// and hall-button axis used throughout [NumFloors][2]bool and [2]RequestState.
// They intentionally match int(elevatordriver.BT_HallUp) and int(elevatordriver.BT_HallDown).
const (
	HallUp   = 0
	HallDown = 1
)

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

type Config struct {
	DoorOpenDuration time.Duration
}

type ElevatorInitState struct {
	CabRequests []bool
	DoorOpen    bool
}

type Elevator struct {
	Behaviour      ElevatorBehaviour
	Floor          int
	Direction      elevatordriver.MotorDirection
	CabRequests    [NumFloors]bool
	HallRequests   [NumFloors][2]bool
	IsOutOfService bool
	Config         Config
}

type ElevatorIn struct {
	Floor             <-chan int
	CabRequests       <-chan []bool
	HallRequests      <-chan [][2]bool
	DoorClosed        <-chan struct{}
	MotorStall        <-chan struct{}
	ElevatorInitState <-chan ElevatorInitState
}

type ElevatorOut struct {
	NetworkState      chan<- Elevator
	LightsState       chan<- Elevator
	ServedRequests    chan<- elevatordriver.ButtonEvent
	DoorOpen          chan<- struct{}
	ResetMotorTimer   chan<- struct{}
	StopMotorTimer    chan<- struct{}
	ConfirmDoorClosed chan<- struct{}
}

func ElevatorUninitialized(cabRequests [NumFloors]bool) *Elevator {
	return &Elevator{
		Floor:       elevatordriver.GetFloor(),
		Direction:   elevatordriver.MD_Stop,
		Behaviour:   EB_Idle,
		Config:      Config{DoorOpenDuration: 3 * time.Second},
		CabRequests: cabRequests,
	}
}

// ---- Command pattern ----

type ElevatorCommand interface {
	execute(out ElevatorOut)
}

type CmdSetMotorDirectionCmd struct{ Dir elevatordriver.MotorDirection }
type CmdDoorRequestCmd struct{}

func (c CmdSetMotorDirectionCmd) execute(out ElevatorOut) {
	elevatordriver.SetMotorDirection(c.Dir)
	if c.Dir != elevatordriver.MD_Stop {
		out.ResetMotorTimer <- struct{}{}
	} else {
		select {
		case out.StopMotorTimer <- struct{}{}:
		default:
		}
	}
}

func (c CmdDoorRequestCmd) execute(out ElevatorOut) {
	out.DoorOpen <- struct{}{}
}

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
		if elevator.HallRequests[f][HallUp] {
			hUp = "^"
		}
		hDn := "-"
		if elevator.HallRequests[f][HallDown] {
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
	fmt.Printf("  Direction: %s\n", DirnToString(elevator.Direction))
}

func DirnToString(d elevatordriver.MotorDirection) string {
	switch d {
	case elevatordriver.MD_Up:
		return "up"
	case elevatordriver.MD_Down:
		return "down"
	default:
		return "stop"
	}
}

func ButtonToString(b elevatordriver.ButtonType) string {
	switch b {
	case elevatordriver.BT_HallUp:
		return "HallUp"
	case elevatordriver.BT_HallDown:
		return "HallDown"
	case elevatordriver.BT_Cab:
		return "Cab"
	default:
		return "Unknown"
	}
}
