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
	hallButtonChan <-chan elevio.ButtonEvent,
	elevatorStateChan <-chan elevatorcontroller.Elevator,
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

		case elevatorState := <-elevatorStateChan:
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState)

		case hallButton := <-hallButtonChan:
			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)

		case peerUpdate := <-peerUpdateCh:
			fmt.Printf("Peer update: peers=%q new=%q lost=%q\n", peerUpdate.Peers, peerUpdate.New, peerUpdate.Lost)
			if peerUpdate.New == id {
				currentSnapshot = flipOwnUnknownsToInactive(currentSnapshot, id)
			}
			removeLostPeers(knownStates, peerUpdate.Lost)

		case receivedSnapshot := <-snapshotRx:
			fmt.Printf("Received snapshot from %s (iter %d)\n", receivedSnapshot.NodeID, receivedSnapshot.Iter)
			if isStaleSnapshot(knownStates, receivedSnapshot) {
				break
			}
			knownStates[receivedSnapshot.NodeID] = receivedSnapshot
			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot)
			currentSnapshot = AdvanceToActive(currentSnapshot, collectActivePeerIDs(id, knownStates))
			snapshotChan <- currentSnapshot
		}
	}
}

// hallButtonIndex returns 0 for HallUp and 1 for HallDown,
// matching the [][2]RequestState button-axis convention.
func hallButtonIndex(button elevio.ButtonType) int {
	if button == elevio.BT_HallDown {
		return 1
	}
	return 0
}

// markHallButtonAsRequested sets the pressed hall button to REQUESTED on the
// node's own hall-request entry, provided it has not already advanced further.
func markHallButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevio.ButtonEvent) NetworkSnapshot {
	buttonIndex := hallButtonIndex(button.Button)
	ownRequests := snapshot.HallRequests[nodeID]
	isAlreadyAdvanced := ownRequests[button.Floor][buttonIndex] >= REQUESTED
	if !isAlreadyAdvanced {
		ownRequests[button.Floor][buttonIndex] = REQUESTED
		snapshot.HallRequests[nodeID] = ownRequests
	}
	return snapshot
}

// flipOwnUnknownsToInactive resets all UNKNOWN entries in the node's own
// hall-request row to INACTIVE. Called when the node first appears on the
// peer list, signalling that no hall requests were pending before joining.
func flipOwnUnknownsToInactive(snapshot NetworkSnapshot, nodeID string) NetworkSnapshot {
	snapshot.HallRequests[nodeID] = withUnknownsFlippedToInactive(snapshot.HallRequests[nodeID])
	return snapshot
}

// removeLostPeers deletes each lost peer's snapshot from the known-states map.
func removeLostPeers(knownStates map[string]NetworkSnapshot, lostPeerIDs []string) {
	for _, lostPeerID := range lostPeerIDs {
		delete(knownStates, lostPeerID)
	}
}

// isStaleSnapshot returns true when the received snapshot is a duplicate or
// out-of-order message that has already been processed.
func isStaleSnapshot(knownStates map[string]NetworkSnapshot, received NetworkSnapshot) bool {
	lastKnown, hasBeenSeen := knownStates[received.NodeID]
	return hasBeenSeen && received.Iter <= lastKnown.Iter
}

// collectActivePeerIDs builds the full list of peer IDs (self + all known peers)
// used by AdvanceToActive to evaluate whether consensus has been reached.
func collectActivePeerIDs(localID string, knownStates map[string]NetworkSnapshot) []string {
	peerIDs := make([]string, 0, len(knownStates)+1)
	peerIDs = append(peerIDs, localID)
	for peerID := range knownStates {
		peerIDs = append(peerIDs, peerID)
	}
	return peerIDs
}

func newNetworkSnapshot(initState elevatorcontroller.Elevator, id string) NetworkSnapshot {
	ownHallRequests := make([][2]RequestState, elevatorcontroller.NumFloors)
	return NetworkSnapshot{
		NodeID: id,
		HallRequests: map[string][][2]RequestState{
			id: ownHallRequests,
		},
		Elevators: map[string]ElevatorState{
			id: LocalElevatorToElevatorState(initState),
		},
		Iter: 0,
	}
}
