package driver

import (
	elevatorcontroller "Heislab/ElevatorController"
	types "Heislab/Types"
	"Heislab/driver-go/elevio"
)

// StartElevator initializes the hardware driver, creates all necessary channels,
// starts polling goroutines, and launches the elevator controller.
//
// WARNING: this function starts the four hardware polling goroutines
// (PollButtons, PollFloorSensor, PollObstructionSwitch, PollStopButton).
// Driver() in driver.go does the same. They must never both be called:
// the elevator_io TCP connection is shared and the mutex only serializes
// individual request-response pairs — events would be silently split
// between the two sets of goroutines and the controller would miss inputs.
//
// localID must be the value returned by network.ResolveLocalID(). It is
// stamped as SenderID on every outgoing NetworkPacket so that receivers
// can correlate packets with the peer list from peers.Receiver.
//
// Returns:
//   - hallButtonChan:  hall button presses for the Manager
//   - hallRequestChan: assigned hall requests into the elevator
//   - packetChan:      ready-to-broadcast NetworkPackets for network.BroadcastElevatorState
func StartElevator(addr string, numFloors int, localID string) (
	hallButtonChan <-chan elevio.ButtonEvent,
	hallRequestChan chan<- [elevatorcontroller.NumFloors]elevatorcontroller.HallRequestDirectionPair,
	packetChan <-chan types.NetworkPacket,
) {
	// Initialize hardware
	elevio.Init(addr, numFloors)

	// Create driver channels
	drv_buttons := make(chan elevio.ButtonEvent)
	drv_floors := make(chan int)
	drv_obstr := make(chan bool)
	drv_stop := make(chan bool)

	// Start hardware polling goroutines
	go elevio.PollButtons(drv_buttons)
	go elevio.PollFloorSensor(drv_floors)
	go elevio.PollObstructionSwitch(drv_obstr)
	go elevio.PollStopButton(drv_stop)

	// Create manager communication channels
	hallBtnChan := make(chan elevio.ButtonEvent)
	hallReqChan := make(chan [elevatorcontroller.NumFloors]elevatorcontroller.HallRequestDirectionPair)

	// snapshotChan carries raw snapshots from the FSM to the packet wrapper goroutine.
	// Buffered so the FSM non-blocking send always succeeds when the wrapper is busy.
	snapshotChan := make(chan types.ElevatorStatesNetworkSnapshot, 8)

	// wrappedChan carries fully formed NetworkPackets to the network transmitter.
	wrappedChan := make(chan types.NetworkPacket, 8)

	// Wrap snapshots: stamp localID + cyclic sequence counter
	go wrapSnapshots(localID, snapshotChan, wrappedChan)

	// Start elevator controller
	go elevatorcontroller.RunElevator(
		drv_buttons,
		drv_floors,
		drv_obstr,
		drv_stop,
		hallBtnChan,
		hallReqChan,
		snapshotChan,
	)

	return hallBtnChan, hallReqChan, wrappedChan
}

const sequenceMax uint8 = 10

// wrapSnapshots reads raw snapshots from in, wraps each one into a NetworkPacket
// with the given senderID and an incrementing cyclic sequence counter, and
// forwards the packet to out.
func wrapSnapshots(senderID string, in <-chan types.ElevatorStatesNetworkSnapshot, out chan<- types.NetworkPacket) {
	var seq uint8
	for snapshot := range in {
		out <- types.NetworkPacket{
			SenderID:   senderID,
			SequenceID: seq,
			Payload:    snapshot,
		}
		if seq == sequenceMax {
			seq = 0
		} else {
			seq++
		}
	}
}
