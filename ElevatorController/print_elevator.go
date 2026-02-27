package elevatorcontroller

import (
	"Heislab/driver-go/elevio"
	"fmt"
)

type Direction struct {
	elevio.MotorDirection
}
type Button struct {
	elevio.ButtonType
}

func (direction Direction) DirString() string {
	switch direction.MotorDirection {
	case elevio.MD_Up:
		return "up"
	case elevio.MD_Down:
		return "down"
	case elevio.MD_Stop:
		return "stop"
	default:
		return "unknown"
	}
}

// ButtonToString converts a ButtonType to a string
func (button Button) BtnString() string {
	switch button {
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
	fmt.Printf("  Direction: %s\n", elevator.Direction.DirString())
}
