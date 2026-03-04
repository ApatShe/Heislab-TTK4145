package main

import (
	elevatorcontroller "Heislab/ElevatorController"
	door "Heislab/Hardware"
	"Heislab/Network/network/localip"
	"Heislab/Network/network/peers"
	networkdriver "Heislab/NetworkDriver"
	"Heislab/driver-go/elevio"
	"Heislab/lights"
	"Heislab/manager"
	"Heislab/timer"
	"flag"
	"fmt"
	"strconv"
	"time"
)

func main() {

	const (
		defaultElevatorPort = 15657
		obstructionTimeout  = time.Second * 10
		motorTimeout        = time.Second * 10
	)

	// ---- Flags ----
	var port int
	flag.IntVar(&port, "port", defaultElevatorPort, "Simulator TCP port")

	// id uniquely identifies this node on the network. Defaults to local IP so
	// physical machines are automatically distinct. Override with --id when
	// running multiple instances on the same machine.
	localIP, err := localip.LocalIP()
	if err != nil {
		panic(fmt.Sprintf("could not resolve local IP: %v", err))
	}
	var id string
	flag.StringVar(&id, "id", localIP, "Network node id (default: local IP)")

	flag.Parse()
	fmt.Printf("Starting node id=%s port=%d\n", id, port)

	// ---- Initialize elevator IO ----
	elevio.Init("localhost:"+strconv.Itoa(port), elevatorcontroller.NumFloors)
	initElevator := *elevatorcontroller.ElevatorUninitialized()
	doorOpenDuration := initElevator.Config.DoorOpenDuration

	// ---- Hardware event channels ----
	buttonEventChan := make(chan elevio.ButtonEvent, 1)
	floorChan := make(chan int)
	obstructionChan := make(chan bool)

	go elevio.PollButtons(buttonEventChan)
	go elevio.PollFloorSensor(floorChan)
	go elevio.PollObstructionSwitch(obstructionChan)

	// Don't block — default to no obstruction if switch hasn't fired yet
	obstructionInit := false
	select {
	case obstructionInit = <-obstructionChan:
	default:
	}

	// ---- Timer signal channels (chan struct{} — receiving the signal IS the message) ----
	resetDoorTimerChan := make(chan struct{})
	stopDoorTimerChan := make(chan struct{})
	doorTimerExpiredChan := make(chan struct{}, 1)
	go timer.RunTimer(resetDoorTimerChan, stopDoorTimerChan, doorTimerExpiredChan, doorOpenDuration, false, "Door Timer")

	resetObstructionWatchdogChan := make(chan struct{})
	stopObstructionWatchdogChan := make(chan struct{})
	go timer.RunTimer(resetObstructionWatchdogChan, stopObstructionWatchdogChan, nil, obstructionTimeout, true, "Obstruction Watchdog")

	resetMotorWatchdogChan := make(chan struct{})
	stopMotorWatchdogChan := make(chan struct{})
	motorStallChan := make(chan struct{}, 1)
	go timer.RunTimer(resetMotorWatchdogChan, stopMotorWatchdogChan, motorStallChan, motorTimeout, false, "Motor Watchdog")

	// ---- Inter-goroutine channels ----
	hallButtonChan := make(chan elevio.ButtonEvent, 1)             // hall presses  → network node
	cabOrderChan := make(chan elevio.ButtonEvent, 1)               // cab presses   → elevator
	hallRequestChan := make(chan [][2]bool, 1)                     // HRA matrix    → elevator
	elevatorStateChan := make(chan elevatorcontroller.Elevator, 1) // FSM state     → network node
	lightsStateChan := make(chan elevatorcontroller.Elevator, 1)   // FSM state     → lights
	servedHallChan := make(chan elevio.ButtonEvent, 4)             // served halls  → network node

	snapshotChan := make(chan networkdriver.NetworkSnapshot, 1) // consensus     → manager
	peerUpdateChan := make(chan peers.PeerUpdate, 1)            // peer list     → manager
	hallLightsChan := make(chan [][2]bool, 1)                   // HRA matrix    → lights

	doorOpenRequestChan := make(chan struct{}) // FSM → door module
	doorClosedChan := make(chan struct{})      // door module → FSM
	doorLampChan := make(chan bool, 1)         // door module → lights
	doorInitChan := make(chan bool, 1)         // manager → door module (restart recovery)

	initChan := make(chan struct{}, 1) // network node → elevator (safe to start)

	// ---- Button router: split hardware poll into cab (local) and hall (network) ----
	go func() {
		for btn := range buttonEventChan {
			if btn.Button == elevio.BT_Cab {
				cabOrderChan <- btn
			} else {
				hallButtonChan <- btn
			}
		}
	}()

	// ---- Spawn core goroutines ----
	go networkdriver.RunNetworkNode(
		networkdriver.NetworkNodeIn{
			HallButton:    hallButtonChan,
			ElevatorState: elevatorStateChan,
			ServedHall:    servedHallChan,
		},
		networkdriver.NetworkNodeOut{
			Snapshot:   snapshotChan,
			PeerUpdate: peerUpdateChan,
			Init:       initChan,
		},
		initElevator,
		id,
	)

	go manager.RunManager(
		manager.ManagerIn{
			Snapshot:   snapshotChan,
			PeerUpdate: peerUpdateChan,
		},
		manager.ManagerOut{
			HallRequests: hallRequestChan,
			HallLights:   hallLightsChan,
			DoorInit:     doorInitChan,
		},
		id,
	)

	go elevatorcontroller.RunElevator(
		elevatorcontroller.ElevatorIn{
			Floor:        floorChan,
			CabButton:    cabOrderChan,
			HallRequests: hallRequestChan,
			DoorClosed:   doorClosedChan,
			MotorStall:   motorStallChan,
			Init:         initChan,
		},
		elevatorcontroller.ElevatorOut{
			NetworkState:    elevatorStateChan,
			LightsState:     lightsStateChan,
			ServedHall:      servedHallChan,
			DoorOpen:        doorOpenRequestChan,
			ResetMotorTimer: resetMotorWatchdogChan,
			StopMotorTimer:  stopMotorWatchdogChan,
		},
	)

	go door.RunDoor(
		door.DoorIn{
			Obstruction:          obstructionChan,
			TimerExpired:         doorTimerExpiredChan,
			OpenRequest:          doorOpenRequestChan,
			NetworkDoorOpenState: doorInitChan,
		},
		door.DoorOut{
			Closed:                   doorClosedChan,
			Lamp:                     doorLampChan,
			ResetTimer:               resetDoorTimerChan,
			ResetObstructionWatchdog: resetObstructionWatchdogChan,
			StopObstructionWatchdog:  stopObstructionWatchdogChan,
		},
		obstructionInit,
	)

	go lights.RunLights(lights.LightsIn{
		ElevatorState: lightsStateChan,
		HallRequests:  hallLightsChan,
		DoorLamp:      doorLampChan,
	})

	select {}
}
