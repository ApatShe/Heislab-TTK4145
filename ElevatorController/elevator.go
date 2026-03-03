package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
	"time"
)

const NumFloors = 4

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

type Elevator struct {
	Behaviour    ElevatorBehaviour
	Floor        int
	Direction    elevio.MotorDirection
	CabRequests  [NumFloors]bool
	HallRequests [NumFloors][2]bool // [floor][0=up, 1=down]
	Config       Config
}

func ElevatorUninitialized() *Elevator {
	return &Elevator{
		Floor:     -1,
		Direction: elevio.MD_Stop,
		Behaviour: EB_Idle,
		Config:    Config{DoorOpenDuration: 3 * time.Second},
	}
}

// InitBetweenFloors moves the motor down until a floor is reached and returns
// the initial elevator state together with the door-open duration for use by
// the door timer.
func InitBetweenFloors() (Elevator, time.Duration) {
	e := ElevatorUninitialized()
	if elevio.GetFloor() == -1 {
		elevio.SetMotorDirection(elevio.MD_Down)
		e.Direction = elevio.MD_Down
		e.Behaviour = EB_Moving
	}
	return *e, e.Config.DoorOpenDuration
}

// ---- Command pattern ----

type CommandType int

const (
	CmdSetMotorDirection CommandType = iota
	CmdSetFloorIndicator
	CmdDoorRequest
)

type ElevatorCommand struct {
	Type  CommandType
	Value any // typed per CommandType: MotorDirection for CmdSetMotorDirection, int for the rest
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
		if elevator.HallRequests[f][0] {
			hUp = "^"
		}
		hDn := "-"
		if elevator.HallRequests[f][1] {
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

func DirnToString(d elevio.MotorDirection) string {
	switch d {
	case elevio.MD_Up:
		return "up"
	case elevio.MD_Down:
		return "down"
	default:
		return "stop"
	}
}

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
