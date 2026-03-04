package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
	"time"
)

const NumFloors = 4

// HallUp and HallDown are the array indices for the two-element hall-request
// and hall-button axis used throughout [NumFloors][2]bool and [2]RequestState.
// They intentionally match int(elevio.BT_HallUp) and int(elevio.BT_HallDown).
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

type Elevator struct {
	Behaviour    ElevatorBehaviour
	Floor        int
	Direction    elevio.MotorDirection
	CabRequests  [NumFloors]bool
	HallRequests [NumFloors][2]bool // [floor][0=up, 1=down]
	Config       Config
}

// ElevatorIn groups all channels that deliver events into RunElevator.
// Inputs arrive from: hardware polling, the manager (HRA output), the door
// module, the motor watchdog, and the network node (init signal).
type ElevatorIn struct {
	Floor        <-chan int
	CabButton    <-chan elevio.ButtonEvent
	HallRequests <-chan [][2]bool
	DoorClosed   <-chan struct{}
	MotorStall   <-chan struct{}
	Init         <-chan struct{}
}

// ElevatorOut groups all channels that RunElevator writes into.
type ElevatorOut struct {
	NetworkState    chan<- Elevator           // broadcast to RunNetworkNode
	LightsState     chan<- Elevator           // broadcast to RunLights
	ServedHall      chan<- elevio.ButtonEvent // cleared hall requests → RunNetworkNode
	DoorOpen        chan<- struct{}           // open-door signal → RunDoor
	ResetMotorTimer chan<- struct{}           // keep motor watchdog alive
	StopMotorTimer  chan<- struct{}           // disarm motor watchdog when motor stops
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
	elevator := ElevatorUninitialized()
	if elevio.GetFloor() == -1 {
		elevio.SetMotorDirection(elevio.MD_Down)
		elevator.Direction = elevio.MD_Down
		elevator.Behaviour = EB_Moving
	}
	return *elevator, elevator.Config.DoorOpenDuration
}

// ---- Command pattern ----

type ElevatorCommand interface {
	execute(out ElevatorOut)
}

type CmdSetMotorDirectionCmd struct{ Dir elevio.MotorDirection }
type CmdSetFloorIndicatorCmd struct{ Floor int }
type CmdDoorRequestCmd struct{}

func (c CmdSetMotorDirectionCmd) execute(out ElevatorOut) {
	elevio.SetMotorDirection(c.Dir)
	if c.Dir != elevio.MD_Stop {
		out.ResetMotorTimer <- struct{}{}
	} else {
		select {
		case out.StopMotorTimer <- struct{}{}:
		default:
		}
	}
}

func (c CmdSetFloorIndicatorCmd) execute(out ElevatorOut) {
	elevio.SetFloorIndicator(c.Floor)
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
