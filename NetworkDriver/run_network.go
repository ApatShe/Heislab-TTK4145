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
	in NetworkNodeIn,
	out NetworkNodeOut,
	initState elevatorcontroller.Elevator,
	id string,
) {
	currentSnapshot := newNetworkSnapshot(initState, id)
	knownStates := make(map[string]NetworkSnapshot)
	iter := uint64(0)

	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool, 1)
	snapshotTx := make(chan NetworkSnapshot)
	snapshotRx := make(chan NetworkSnapshot)

	peerTxEnable <- false // hold off broadcasting until own state is recovered

	go peers.Transmitter(peerUpdateBroadcastPort, id, peerTxEnable)
	go peers.Receiver(peerUpdateBroadcastPort, peerUpdateCh)

	go bcast.Transmitter(snaphotBroadCastPort, snapshotTx)
	go bcast.Receiver(snaphotBroadCastPort, snapshotRx)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// If no peer snapshot arrives within 2 seconds we are alone — safe to start.
	startupTimer := time.NewTimer(2 * time.Second)
	defer startupTimer.Stop()

	readyToBroadcast := false

	enableBroadcast := func() {
		if !readyToBroadcast {
			readyToBroadcast = true
			peerTxEnable <- true
			select {
			case out.Init <- struct{}{}:
			default:
			}
		}
	}

	for {
		select {

		case <-ticker.C:
			if !readyToBroadcast {
				break
			}
			iter++
			currentSnapshot.Iter = iter
			fmt.Printf("[TX] iter=%d hallRequests=%v elevators=%v\n", iter, currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id])
			snapshotTx <- currentSnapshot

		case <-startupTimer.C:
			// No peer responded in time — treat as single-elevator startup.
			enableBroadcast()

		case elevatorState := <-in.ElevatorState:
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState)

		case hallButton := <-in.HallButton:
			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)
			currentSnapshot = AdvanceToActive(currentSnapshot, collectActivePeerIDs(id, knownStates))
			select {
			case out.Snapshot <- currentSnapshot:
			default:
			}

		case served := <-in.ServedHall:
			// The elevator has served this hall request — reset it to INACTIVE so
			// the cyclic counter can propagate the clear to all peers.
			buttonIndex := hallButtonIndex(served.Button)
			ownRequests := currentSnapshot.HallRequests[id]
			if ownRequests != nil && ownRequests[served.Floor][buttonIndex] == ACTIVE {
				ownRequests[served.Floor][buttonIndex] = INACTIVE
				currentSnapshot.HallRequests[id] = ownRequests
			}

		case peerUpdate := <-peerUpdateCh:
			fmt.Printf("=== PEER UPDATE === peers=%q  NEW=%q  LOST=%q\n", peerUpdate.Peers, peerUpdate.New, peerUpdate.Lost)

			// If we are the only peer on the network there is no state to recover
			// from — safe to start broadcasting immediately.
			if isAloneOnNetwork(peerUpdate) {
				enableBroadcast()
			}

			removeLostPeers(knownStates, peerUpdate.Lost)

			select {
			case out.PeerUpdate <- peerUpdate:
			default:
			}
		case receivedSnapshot := <-snapshotRx:
			fmt.Printf("Received snapshot from %s (iter %d)\n", receivedSnapshot.NodeID, receivedSnapshot.Iter)
			if isStaleSnapshot(knownStates, receivedSnapshot) {
				fmt.Printf("  [stale, skipping]\n")
				break
			}
			for nodeID, elev := range receivedSnapshot.Elevators {
				fmt.Printf("  elevator[%s]: floor=%d dir=%s beh=%s\n", nodeID, elev.Floor, elev.Direction, elev.Behaviour)
			}
			fmt.Printf("  hallRequests: %v\n", receivedSnapshot.HallRequests)
			knownStates[receivedSnapshot.NodeID] = receivedSnapshot
			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot)
			currentSnapshot = adoptHallRequestsFromPeers(currentSnapshot)
			currentSnapshot = AdvanceToActive(currentSnapshot, collectActivePeerIDs(id, knownStates))
			select {
			case out.Snapshot <- currentSnapshot:
			default:
			}
			// We have absorbed at least one peer snapshot — own UNKNOWN cabs have
			// been recovered via mergeCabRequests. Safe to start broadcasting.
			enableBroadcast()
		}
	}
}

// hallButtonIndex converts a ButtonType to the corresponding array index used
// in [][2]RequestState. Since BT_HallUp=0 and BT_HallDown=1 the cast is
// direct; the function exists to make the intent explicit at call sites.
func hallButtonIndex(button elevio.ButtonType) int {
	return int(button)
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

// isAloneOnNetwork returns true when the peer list contains only one entry
// (self), meaning there are no other nodes to recover state from.
func isAloneOnNetwork(peerUpdate peers.PeerUpdate) bool {
	return len(peerUpdate.Peers) == 1
}

func newNetworkSnapshot(initState elevatorcontroller.Elevator, id string) NetworkSnapshot {
	// Own cab requests are initialized as UNKNOWN so that mergeCabRequests can
	// recover the correct state from a peer's snapshot before first broadcast.
	ownCabRequests := make([]RequestState, elevatorcontroller.NumFloors)
	ownElevatorState := ElevatorState{
		Behaviour:   initState.Behaviour.String(),
		Floor:       initState.Floor,
		Direction:   elevatorcontroller.DirnToString(initState.Direction),
		CabRequests: ownCabRequests, // all UNKNOWN (zero value)
	}
	ownHallRequests := make([][2]RequestState, elevatorcontroller.NumFloors)
	for i := range ownHallRequests {
		ownHallRequests[i] = [2]RequestState{INACTIVE, INACTIVE}
	}
	return NetworkSnapshot{
		NodeID: id,
		HallRequests: map[string][][2]RequestState{
			id: ownHallRequests,
		},
		Elevators: map[string]ElevatorState{
			id: ownElevatorState,
		},
		Iter: 0,
	}
}
