package main

import (
	"Heislab/coordinator"
	door "Heislab/door"
	elevatorcontroller "Heislab/elevatorcontroller"
	elevatordriver "Heislab/elevatordriver"
	"Heislab/lights"
	networknode "Heislab/networknode"
	"Heislab/node_communication/bcast"
	"Heislab/node_communication/localip"
	"Heislab/node_communication/peers"
	"Heislab/timer"
	"flag"
	"fmt"
	"net"
	"strconv"
	"time"
)

// ---- Configuration -------------------------------------------------------

type NodeConfig struct {
	Port           int
	PeerRXPort     int
	SnapshotRXPort int
	ID             string
	LocalMode      bool
}

const (
	defaultElevatorPort = 15657
	DOOR_OPEN_DURATION  = 3 * time.Second
	obstructionTimeout  = 10 * time.Second
	motorTimeout        = 10 * time.Second
)

func parseFlags() NodeConfig {
	var cfg NodeConfig
	flag.IntVar(&cfg.Port, "port", defaultElevatorPort, "Simulator TCP port") // Unique per instance
	flag.StringVar(&cfg.ID, "id", resolveLocalIP(), "Network node id")
	flag.BoolVar(&cfg.LocalMode, "local", false, "Use subnet broadcast")
	flag.Parse()

	// FIXED: Same for all instances
	cfg.PeerRXPort = 15657     // All RX peer heartbeats here
	cfg.SnapshotRXPort = 15667 // All RX snapshots here

	return cfg
}

func resolveLocalIP() string {
	ip, err := localip.LocalIP()
	if err != nil {
		panic(fmt.Sprintf("could not resolve local IP: %v", err))
	}
	return ip
}

func configureNetwork(cfg NodeConfig) {
	if cfg.LocalMode {
		addr := subnetBroadcastAddr(resolveLocalIP())
		fmt.Printf("[local mode] using broadcast address %s\n", addr)
		bcast.SetBroadcastAddr(addr)
		peers.SetBroadcastAddr(addr)
	}
	fmt.Printf("Starting node id=%s port=%d\n", cfg.ID, cfg.Port)
}

// ---- Hardware ------------------------------------------------------------

// HardwareChannels carries raw event streams from the elevator I/O pollers.
type HardwareChannels struct {
	Buttons         chan elevatordriver.ButtonEvent
	Floor           chan int
	Obstruction     chan bool
	ObstructionInit bool
}

func initHardware(port int) {
	elevatordriver.Init("localhost:"+strconv.Itoa(port), elevatorcontroller.NumFloors)
}

func startHardwarePolling() HardwareChannels {
	hw := HardwareChannels{
		Buttons:     make(chan elevatordriver.ButtonEvent, 1),
		Floor:       make(chan int),
		Obstruction: make(chan bool),
	}
	go elevatordriver.PollButtons(hw.Buttons)
	go elevatordriver.PollFloorSensor(hw.Floor)
	go elevatordriver.PollObstructionSwitch(hw.Obstruction)

	// Sample once — default false if switch has not fired yet.
	select {
	case hw.ObstructionInit = <-hw.Obstruction:
	default:
	}
	return hw
}

// ---- Timers and watchdogs ------------------------------------------------

type DoorTimer struct {
	Reset   chan struct{}
	Stop    chan struct{}
	Expired chan struct{}
}

type ObstructionWatchdog struct {
	Reset chan struct{}
	Stop  chan struct{}
}

type MotorWatchdog struct {
	Reset chan struct{}
	Stop  chan struct{}
	Stall chan struct{}
}

func newDoorTimer(duration time.Duration) DoorTimer {
	t := DoorTimer{
		Reset:   make(chan struct{}),
		Stop:    make(chan struct{}),
		Expired: make(chan struct{}, 1),
	}
	go timer.RunTimer(t.Reset, t.Stop, t.Expired, duration, false, "Door Timer")
	return t
}

func newObstructionWatchdog() ObstructionWatchdog {
	w := ObstructionWatchdog{
		Reset: make(chan struct{}),
		Stop:  make(chan struct{}),
	}

	go timer.RunTimer(w.Reset, w.Stop, nil, obstructionTimeout, true, "Obstruction Watchdog")
	return w
}

func newMotorWatchdog() MotorWatchdog {
	w := MotorWatchdog{
		Reset: make(chan struct{}),
		Stop:  make(chan struct{}),
		Stall: make(chan struct{}, 1),
	}
	go timer.RunTimer(w.Reset, w.Stop, w.Stall, motorTimeout, false, "Motor Watchdog")
	return w
}

// ---- Button routing ------------------------------------------------------

// routeButtons splits the unified hardware button stream into cab (local) and
// hall (network) streams. Receiving the event IS the message.
func routeButtons(src <-chan elevatordriver.ButtonEvent, cab, hall chan<- elevatordriver.ButtonEvent) {
	for btn := range src {
		if btn.Button == elevatordriver.BT_Cab {
			cab <- btn
		} else {
			hall <- btn
		}
	}
}

// ---- Subsystem launch ----------------------------------------------------

func launchNetworkNode(
	id string,
	cabButtonIn chan elevatordriver.ButtonEvent,
	hallButtonIn chan elevatordriver.ButtonEvent,
	elevatorStateIn chan elevatorcontroller.Elevator,
	servedRequestsIn chan elevatordriver.ButtonEvent,
	snapshotOut chan networknode.NetworkSnapshot,
	peerUpdateOut chan peers.PeerUpdate,
	ElevatorInitState chan elevatorcontroller.ElevatorInitState,
) {
	go networknode.RunNetworkNode(
		networknode.NetworkNodeIn{
			CabButton:      cabButtonIn,
			HallButton:     hallButtonIn,
			ElevatorState:  elevatorStateIn,
			ServedRequests: servedRequestsIn,
		},
		networknode.NetworkNodeOut{
			Snapshot:          snapshotOut,
			PeerUpdate:        peerUpdateOut,
			ElevatorInitState: ElevatorInitState,
		},
		id,
	)
}

func launchManager(
	id string,
	snapshotIn chan networknode.NetworkSnapshot,
	peerUpdateIn chan peers.PeerUpdate,
	cabRequestOut chan []bool,
	hallRequestOut chan [][2]bool,
	LightsOut chan coordinator.RequestLights,
	doorInitOut chan bool,
) {
	go coordinator.RunManager(
		coordinator.ManagerIn{
			Snapshot:   snapshotIn,
			PeerUpdate: peerUpdateIn,
		},
		coordinator.ManagerOut{
			CabRequests:  cabRequestOut,
			HallRequests: hallRequestOut,
			Lights:       LightsOut,
			DoorInit:     doorInitOut,
		},
		id,
	)
}

func launchElevatorFSM(
	hw HardwareChannels,
	motorWatchdog MotorWatchdog,
	cabRequestFromManager chan []bool,
	hallRequestIn chan [][2]bool,
	doorClosedIn chan struct{},
	elevatorInitState chan elevatorcontroller.ElevatorInitState,
	elevatorStateOut chan elevatorcontroller.Elevator,
	lightsStateOut chan elevatorcontroller.Elevator,
	servedRequestsOut chan elevatordriver.ButtonEvent,
	doorOpenReqOut chan struct{},
) {
	go elevatorcontroller.RunElevator(
		elevatorcontroller.ElevatorIn{
			Floor:             hw.Floor,
			CabRequests:       cabRequestFromManager,
			HallRequests:      hallRequestIn,
			DoorClosed:        doorClosedIn,
			MotorStall:        motorWatchdog.Stall,
			ElevatorInitState: elevatorInitState,
		},
		elevatorcontroller.ElevatorOut{
			NetworkState:    elevatorStateOut,
			LightsState:     lightsStateOut,
			ServedRequests:  servedRequestsOut,
			DoorOpen:        doorOpenReqOut,
			ResetMotorTimer: motorWatchdog.Reset,
			StopMotorTimer:  motorWatchdog.Stop,
		},
	)
}

func launchDoor(
	hw HardwareChannels,
	doorTimer DoorTimer,
	obstructionWatchdog ObstructionWatchdog,
	doorOpenReqIn chan struct{},
	doorInitIn chan bool,
	doorClosedOut chan struct{},
	doorLampOut chan bool,
) {
	go door.RunDoor(
		door.DoorIn{
			Obstruction:          hw.Obstruction,
			TimerExpired:         doorTimer.Expired,
			OpenRequest:          doorOpenReqIn,
			NetworkDoorOpenState: doorInitIn,
		},
		door.DoorOut{
			Closed:                   doorClosedOut,
			Lamp:                     doorLampOut,
			ResetTimer:               doorTimer.Reset,
			ResetObstructionWatchdog: obstructionWatchdog.Reset,
			StopObstructionWatchdog:  obstructionWatchdog.Stop,
		},
		hw.ObstructionInit,
	)
}

func launchLights(
	lightsStateIn chan elevatorcontroller.Elevator,
	Lights chan coordinator.RequestLights,
	doorLampIn chan bool,
) {
	go lights.RunLights(lights.LightsIn{
		ElevatorState: lightsStateIn,
		RequestLights: Lights,
		DoorLamp:      doorLampIn,
	})
}

// ---- Entry point ---------------------------------------------------------

func main() {
	cfg := parseFlags()
	configureNetwork(cfg)

	initHardware(cfg.Port)
	hw := startHardwarePolling()

	doorTimer := newDoorTimer(DOOR_OPEN_DURATION)
	obstructionWatchdog := newObstructionWatchdog()
	motorWatchdog := newMotorWatchdog()

	// -- Network node channels --
	hallButtonCh := make(chan elevatordriver.ButtonEvent, 1)
	elevatorStateCh := make(chan elevatorcontroller.Elevator, 1)
	servedRequestsCh := make(chan elevatordriver.ButtonEvent, 4)
	snapshotCh := make(chan networknode.NetworkSnapshot, 1)
	peerUpdateCh := make(chan peers.PeerUpdate, 1)
	elevatorInitStateCh := make(chan elevatorcontroller.ElevatorInitState, 1)

	// -- Manager channels --
	cabRequestCh := make(chan []bool, 1)
	hallRequestCh := make(chan [][2]bool, 1)
	LightsOut := make(chan coordinator.RequestLights, 1)
	doorInitCh := make(chan bool, 1)

	// -- Elevator FSM ↔ Door --
	cabOrderCh := make(chan elevatordriver.ButtonEvent, 1)
	doorOpenReqCh := make(chan struct{})
	doorClosedCh := make(chan struct{})

	// -- Lights --
	lightsStateCh := make(chan elevatorcontroller.Elevator, 16)
	doorLampCh := make(chan bool, 1)

	go routeButtons(hw.Buttons, cabOrderCh, hallButtonCh)

	launchNetworkNode(
		cfg.ID,
		cabOrderCh,
		hallButtonCh,
		elevatorStateCh,
		servedRequestsCh,
		snapshotCh,
		peerUpdateCh,
		elevatorInitStateCh)

	launchManager(
		cfg.ID,
		snapshotCh,
		peerUpdateCh,
		cabRequestCh,
		hallRequestCh,
		LightsOut,
		doorInitCh)

	launchElevatorFSM(
		hw,
		motorWatchdog,
		cabRequestCh,
		hallRequestCh,
		doorClosedCh,
		elevatorInitStateCh,
		elevatorStateCh,
		lightsStateCh,
		servedRequestsCh,
		doorOpenReqCh)

	launchDoor(
		hw,
		doorTimer,
		obstructionWatchdog,
		doorOpenReqCh,
		doorInitCh,
		doorClosedCh,
		doorLampCh)

	launchLights(
		lightsStateCh,
		LightsOut,
		doorLampCh)

	select {}
}

// ---- Utilities -----------------------------------------------------------

// subnetBroadcastAddr derives the directed broadcast address for the interface
// that owns localIP. Falls back to "255.255.255.255" when no match is found.
func subnetBroadcastAddr(localIP string) string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.String() != localIP {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}
			mask := ipnet.Mask
			broadcast := make(net.IP, 4)
			for i := range ip {
				broadcast[i] = ip[i] | ^mask[i]
			}
			return broadcast.String()
		}
	}
	return "255.255.255.255"
}
