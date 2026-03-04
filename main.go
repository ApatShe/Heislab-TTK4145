package main

import (
	elevatorcontroller "Heislab/ElevatorController"
	door "Heislab/Hardware"
	"Heislab/Network/network/localip"
	"Heislab/Network/network/peers"
	networkdriver "Heislab/NetworkDriver"
	"Heislab/driver-go/elevio"
	"Heislab/manager"
	"Heislab/timer"
	"flag"
	"fmt"
	"strconv"
	"time"
)

// Problems arising during FAT and other testing
// - How to test disconnect :sad
//   we make at home version

// TODO: Final todo list before FAT:
// - Convert door into its own process ✓
// - Fully implement obstruction switch and motor blockage timers ✓
// - Change elevator state sending from continuous to diff ✓
// - Change peer list sending from continuous to diff ✓
// - Implement virtual state for pending requests ✓
// - Fix the request assigner ✓
// - Do a *lot* more testing with packet loss, both working and not working
// - Make the hardwareCommands solution cleaner [?]
// - Gather everything into one repo ✓
// - Review the structure of elevalgo ✓
// - Read the project spec completely, and verify that everything works
// - Do test FAT

// A problem with packet loss:
// -----------------------------
// If there is high packet loss and an elevator disconnects, the other two elevators
// may take requests which haven't been fully acked. This can happen if elevator 1
// takes a request while elevator 3 is "disconnected", elevator 1 then detects that
// only elevator 2 needs to ack, which happens, and then elevator 1 takes the request
// with only an ack from elevator 2. If elevator 2 then dies, the request has not been
// backed up
// One reason why this may potentially not be such a huge problem is that
// each elevator broadcasts its state either way, so there is a large chance
// that elevator 3 will pick up that elevator 1 is taking the request anyways, and then
// if elevator 1 dies, elevator 3 can take over / back up the request

// But in general packet loss will ravage our elevator system
// :DD
func main() {

	const (
		defaultElevatorPort = 15657
		obstructionTimeout  = time.Second * 10
		motorTimeout        = time.Second * 10
	)

	// ---- Flags ----
	var port int
	flag.IntVar(&port, "port", defaultElevatorPort, "Simulator TCP port")

	// id uniquely identifies this node on the network.
	// Defaults to local IP so physical machines are automatically distinct.
	// Override with --id when running multiple instances on the same machine.
	localIP, err := localip.LocalIP()
	if err != nil {
		panic(fmt.Sprintf("could not resolve local IP: %v", err))
	}
	var id string
	flag.StringVar(&id, "id", localIP, "Network node id (default: local IP)")

	flag.Parse()
	fmt.Printf("Starting node id=%s port=%d\n", id, port)

	// // ---- Initialize elevator ----
	elevio.Init("localhost:"+strconv.Itoa(port), elevatorcontroller.NumFloors)
	//elevatorcontroller.InitFsm()
	// InitBetweenFloors is deferred until RunElevator receives from initChan
	// (after peer state is recovered). We still need the config values now.
	initElevator := *elevatorcontroller.ElevatorUninitialized()
	doorOpenDuration := initElevator.Config.DoorOpenDuration

	// ---- Initialize hardware communication ----
	buttonEventChan := make(chan elevio.ButtonEvent, 1)
	floorChan := make(chan int)
	obstructionChan := make(chan bool)

	go elevio.PollButtons(buttonEventChan)
	go elevio.PollFloorSensor(floorChan)
	go elevio.PollObstructionSwitch(obstructionChan)

	obstructionInit := <-obstructionChan

	// ---- Initialize timers ----
	// Door timer
	resetDoorTimerChan := make(chan int)
	stopDoorTimerChan := make(chan int)
	doorTimeoutChan := make(chan int)
	go timer.RunTimer(resetDoorTimerChan, stopDoorTimerChan, doorTimeoutChan, doorOpenDuration, false, "Door Timer")

	// Obstruction timer
	resetObstructionWatchdogTimerChan := make(chan int)
	stopObstructionWatchdogTimerChan := make(chan int)
	go timer.RunTimer(resetObstructionWatchdogTimerChan, stopObstructionWatchdogTimerChan, nil, obstructionTimeout, true, "Obstruction Watchdog")

	// Motor timer
	resetMotorWatchdogTimerChan := make(chan int)
	go timer.RunTimer(resetMotorWatchdogTimerChan, nil, nil, motorTimeout, true, "Motor Watchdog")

	// initChan: signals RunElevator that peer state has been recovered — safe to start motor.
	initChan := make(chan int, 1)

	// ---- Networking node communication ----
	// TODO: Try unbuffering some of these channels and see what happens
	hallButtonChan := make(chan elevio.ButtonEvent, 1) // hall presses → network node → cyclic counter
	cabOrderChan := make(chan elevio.ButtonEvent, 1)   // cab presses  → elevator directly
	hallRequestChan := make(chan [][2]bool, 1)         // HRA matrix   → elevator
	elevatorStateChan := make(chan elevatorcontroller.Elevator, 1)
	snapshotChan := make(chan networkdriver.NetworkSnapshot, 1) // consensus snapshot → manager
	peerUpdateToManagerChan := make(chan peers.PeerUpdate, 1)   // peer updates → manager

	// ---- Door communication ----
	doorRequestChan := make(chan int)
	doorCloseChan   := make(chan int)
	doorLampChan    := make(chan bool, 1)

	// ---- Lights communication
	lightsElevatorStateChan := make(chan elevatorcontroller.Elevator, 1)
	hallLightsChan := make(chan [][2]bool, 1)
	// ---- Button router: split raw hardware poll into cab (local) and hall (network) ----
	go func() {
		for btn := range buttonEventChan {
			if btn.Button == elevio.BT_Cab {
				cabOrderChan <- btn
			} else {
				hallButtonChan <- btn
			}
		}
	}()

	// ---- Disconnect ----
	//disconnectChan := make(chan int, 1)

	// ---- Spawn core threads: networking, elevator, door and lights ----
	go networkdriver.RunNetworkNode(
		hallButtonChan,
		elevatorStateChan,
		snapshotChan,
		peerUpdateToManagerChan,
		initChan,
		initElevator,
		id,
	)

	go manager.RunManager(
		snapshotChan,            // ← consensus snapshot after cyclic counter + AdvanceToActive
		peerUpdateToManagerChan, // ← peer updates from network node
		hallRequestChan,         // ← HRA-assigned [][2]bool matrix → elevator
		hallLightsChan,          // ← HRA-assigned [][2]bool matrix → lights
		id,
	)

	go elevatorcontroller.RunElevator(
		floorChan,
		cabOrderChan,
		hallRequestChan,
		doorCloseChan,
		doorRequestChan,
		lightsElevatorStateChan,
		elevatorStateChan,
		resetMotorWatchdogTimerChan,
		initChan,
	)

	go door.RunDoor(
		obstructionChan,
		doorTimeoutChan,
		doorRequestChan,
		doorCloseChan,
		doorLampChan,
		resetDoorTimerChan,
		resetObstructionWatchdogTimerChan,
		stopObstructionWatchdogTimerChan,
		obstructionInit,
	)

	go lights.RunLights(lightsElevatorStateChan, hallLightsChan, doorLampChan)

	//go disconnector.RunDisconnector(disconnectChan)

	for {
		time.Sleep(time.Second)
	}

}
