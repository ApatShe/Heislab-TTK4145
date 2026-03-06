package networkdriver

import (
	elevatorcontroller "Heislab/ElevatorController"
	log "Heislab/Log"
	"Heislab/Network/network/bcast"
	"Heislab/Network/network/peers"
	"Heislab/driver-go/elevio"
	"time"
)

const (
	peerUpdateBroadcastPort = 15647 // Match vanilla example
	snaphotBroadCastPort    = 12345 // Keep your snapshot port
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
	snapshotTx := make(chan NetworkSnapshot, 1)
	snapshotRx := make(chan NetworkSnapshot)

	peerTxEnable <- false

	go peers.Transmitter(peerUpdateBroadcastPort, id, peerTxEnable)
	go peers.Receiver(peerUpdateBroadcastPort, peerUpdateCh)

	go bcast.Transmitter(snaphotBroadCastPort, snapshotTx)
	go bcast.Receiver(snaphotBroadCastPort, snapshotRx)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	startupTimer := time.NewTimer(3 * time.Second)
	defer startupTimer.Stop()

	readyToBroadcast := false
	isSolo := false
	livePeerIDs := []string{id}

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
			knownStates[id] = currentSnapshot
			log.Log("[TX] iter=%d hallRequests=%v elevators=%v", iter, currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id])

			select {
			case snapshotTx <- currentSnapshot:
			default:
			}

		case <-startupTimer.C:
			enableBroadcast()

		case elevatorState := <-in.ElevatorState:
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState)

		case hallButton := <-in.HallButton:
			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)
			currentSnapshot = AdvanceToActive(currentSnapshot, livePeerIDs, knownStates)

			if readyToBroadcast {
				iter++
				currentSnapshot.Iter = iter
				knownStates[id] = currentSnapshot
				forceSend(snapshotTx, currentSnapshot)
			}

			select {
			case out.Snapshot <- currentSnapshot:
			default:
			}

		case served := <-in.ServedHall:
			buttonIndex := hallButtonIndex(served.Button)

			ownRequests := currentSnapshot.HallRequests[id]

			if ownRequests != nil && ownRequests[served.Floor][buttonIndex] == ACTIVE {
				newRequests := make([][2]RequestState, len(ownRequests))
				copy(newRequests, ownRequests)

				newRequests[served.Floor][buttonIndex] = INACTIVE

				currentSnapshot.HallRequests[id] = newRequests

				if readyToBroadcast {
					iter++
					currentSnapshot.Iter = iter
					knownStates[id] = currentSnapshot
					forceSend(snapshotTx, currentSnapshot)
				}

				select {
				case out.Snapshot <- currentSnapshot:
				default:
				}
			}

		case peerUpdate := <-peerUpdateCh:
			log.Log("PEER UPDATE: peers=%q NEW=%q LOST=%q", peerUpdate.Peers, peerUpdate.New, peerUpdate.Lost)

			wasSolo := isSolo
			isSolo = isAloneOnNetwork(peerUpdate)
			if isSolo && !wasSolo {
				log.Log("Transitioned to solo mode, enabling broadcast")
				enableBroadcast()
			}

			removeLostPeers(knownStates, peerUpdate.Lost)

			// Only update livePeerIDs if there are actual peers reported
			if len(peerUpdate.Peers) > 0 {
				livePeerIDs = make([]string, 0, len(peerUpdate.Peers)+1)
				livePeerIDs = append(livePeerIDs, id)
				for _, peer := range peerUpdate.Peers {
					livePeerIDs = append(livePeerIDs, peer)
				}
				log.Log("livePeerIDs updated to %v", livePeerIDs)
			} else {
				log.Log("peerUpdate.Peers empty, keeping previous livePeerIDs=%v", livePeerIDs)
			}

			select {
			case out.PeerUpdate <- peerUpdate:
			default:
			}

		case receivedSnapshot := <-snapshotRx:
			log.Log("Received snapshot from %s (iter %d)", receivedSnapshot.NodeID, receivedSnapshot.Iter)

			for nodeID, elev := range receivedSnapshot.Elevators {
				log.Log("  elevator[%s]: floor=%d dir=%s beh=%s", nodeID, elev.Floor, elev.Direction, elev.Behaviour)
			}
			log.Log("  hallRequests: %v", receivedSnapshot.HallRequests)

			knownStates[receivedSnapshot.NodeID] = receivedSnapshot
			prevSnapshot := currentSnapshot

			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot)
			currentSnapshot = propagateResetsToOwn(currentSnapshot, receivedSnapshot)
			currentSnapshot = adoptHallRequestsFromPeers(currentSnapshot)
			currentSnapshot = AdvanceToActive(currentSnapshot, livePeerIDs, knownStates)

			if readyToBroadcast && snapshotChanged(prevSnapshot, currentSnapshot, id) {
				iter++
				currentSnapshot.Iter = iter
				knownStates[id] = currentSnapshot
				forceSend(snapshotTx, currentSnapshot)
			}

			select {
			case out.Snapshot <- currentSnapshot:
			default:
			}

			enableBroadcast()
		}
	}
}

func forceSend(ch chan NetworkSnapshot, snapshot NetworkSnapshot) {
	select {
	case ch <- snapshot:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func hallButtonIndex(button elevio.ButtonType) int {
	return int(button)
}

func markHallButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevio.ButtonEvent) NetworkSnapshot {

	log.Log("markHallButtonAsRequested: nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)

	buttonIndex := hallButtonIndex(button.Button)

	if _, ok := snapshot.HallRequests[nodeID]; !ok {

		log.Log("markHallButtonAsRequested: nodeID=%s not found in HallRequests, returning early", nodeID)

		return snapshot
	}

	ownRequests := snapshot.HallRequests[nodeID]
	if ownRequests[button.Floor][buttonIndex] < REQUESTED {
		newReqs := make([][2]RequestState, len(ownRequests))
		copy(newReqs, ownRequests)

		newReqs[button.Floor][buttonIndex] = REQUESTED
		snapshot.HallRequests[nodeID] = newReqs

		log.Log("markHallButtonAsRequested: marked as REQUESTED for nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)
	} else {
		log.Log("markHallButtonAsRequested: floor=%d btn=%d already at state=%d, skipping", button.Floor, button.Button, ownRequests[button.Floor][buttonIndex])
	}
	return snapshot
}

func removeLostPeers(knownStates map[string]NetworkSnapshot, lostPeerIDs []string) {
	for _, lostPeerID := range lostPeerIDs {
		delete(knownStates, lostPeerID)
	}
}

func isAloneOnNetwork(peerUpdate peers.PeerUpdate) bool {
	return len(peerUpdate.Peers) == 0
}

func snapshotChanged(before, after NetworkSnapshot, id string) bool {
	beforeReqs := before.HallRequests[id]
	afterReqs := after.HallRequests[id]
	if beforeReqs == nil || afterReqs == nil {
		return false
	}
	for floor := range afterReqs {
		for btn := range afterReqs[floor] {
			if afterReqs[floor][btn] != beforeReqs[floor][btn] {
				return true
			}
		}
	}
	return false
}

func newNetworkSnapshot(initState elevatorcontroller.Elevator, id string) NetworkSnapshot {
	ownCabRequests := make([]RequestState, elevatorcontroller.NumFloors)
	ownElevatorState := ElevatorState{
		Behaviour:   initState.Behaviour.String(),
		Floor:       initState.Floor,
		Direction:   elevatorcontroller.DirnToString(initState.Direction),
		CabRequests: ownCabRequests,
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
