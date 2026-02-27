package networkdriver

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/Network/network/bcast"
	"Heislab/Network/network/peers"
	"Heislab/driver-go/elevio"
	"fmt"
	"time"
)

const (
	peerUpdateBroadcastPort = 36251
	snaphotBroadCastPort    = 12345
)

func RunNetworkNode(
	elevatorStateChan <-chan elevatorcontroller.Elevator,
	orderChan chan<- elevio.ButtonEvent,
	snapshotChan chan<- NetworkSnapshot,
	initState elevatorcontroller.Elevator,
	id string,
) {
	currentSnapshot := newNetworkSnapshot(initState, id)
	knownStates := make(map[string]NetworkSnapshot)
	iter := uint64(0)

	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)
	snapshotTx := make(chan NetworkSnapshot)
	snapshotRx := make(chan NetworkSnapshot)

	go peers.Transmitter(peerUpdateBroadcastPort, id, peerTxEnable)
	go peers.Receiver(peerUpdateBroadcastPort, peerUpdateCh)

	go bcast.Transmitter(snaphotBroadCastPort, snapshotTx)
	go bcast.Receiver(snaphotBroadCastPort, snapshotRx)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {

		case <-ticker.C:
			iter++
			currentSnapshot.Iter = iter
			snapshotTx <- currentSnapshot

		case state := <-elevatorStateChan:
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(state)
			// No tx here — ticker handles broadcasting

		case peer := <-peerUpdateCh:
			fmt.Printf("Peer update:\n")
			fmt.Printf("  Peers: %q\n", peer.Peers)
			fmt.Printf("  New:   %q\n", peer.New)
			fmt.Printf("  Lost:  %q\n", peer.Lost)
			for _, lost := range peer.Lost {
				delete(knownStates, lost)
			}

		case receivedSnapshot := <-snapshotRx:
			fmt.Printf("Received snapshot:\n")
			fmt.Printf("  ID:           %s\n", receivedSnapshot.NodeID)
			fmt.Printf("  Behaviour:    %s\n", receivedSnapshot.Elevators[receivedSnapshot.NodeID].Behaviour)
			fmt.Printf("  Floor:        %d\n", receivedSnapshot.Elevators[receivedSnapshot.NodeID].Floor)
			fmt.Printf("  Direction:    %s\n", receivedSnapshot.Elevators[receivedSnapshot.NodeID].Direction)
			fmt.Printf("  CabRequests:  %v\n", receivedSnapshot.Elevators[receivedSnapshot.NodeID].CabRequests)
			fmt.Printf("  HallRequests: %v\n", receivedSnapshot.HallRequests)
			fmt.Printf("Received snapshot from %s (iter %d)\n", receivedSnapshot.NodeID, receivedSnapshot.Iter)

			// Deduplicate — skip if we've already processed this iter from this peer
			if last, seen := knownStates[receivedSnapshot.NodeID]; seen && receivedSnapshot.Iter <= last.Iter {
				break
			}
			knownStates[receivedSnapshot.NodeID] = receivedSnapshot
			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot)

			snapshotChan <- currentSnapshot
		}
	}
}

func newNetworkSnapshot(initState elevatorcontroller.Elevator, id string) NetworkSnapshot {
	return NetworkSnapshot{
		NodeID:       id,
		HallRequests: make(map[string][][2]RequestState, elevatorcontroller.NumFloors),
		Elevators: map[string]ElevatorState{
			id: LocalElevatorToElevatorState(initState),
		},
		Iter: 0,
	}
}
