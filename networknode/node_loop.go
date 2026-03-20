package networknode

import (
	elevatorcontroller "Heislab/elevatorcontroller"
	elevatordriver "Heislab/elevatordriver"
	"Heislab/node_communication/bcast"
	"Heislab/node_communication/peers"

	"time"
)

const (
	peerPort     = 15657
	snapshotPort = 15667

	// reconnectCooldownTicks is the number of 10 ms ticks that ReconnectedNode
	// stays true after a node re-joins the network. 100 ticks = 1 second gives
	// the network enough time to converge on the reconnecting node's true debts
	// before it starts accepting reset signals from peers again.
	reconnectCooldownTicks = 100
)

func RunNetworkNode(
	in NetworkNodeIn,
	out NetworkNodeOut,
	id string,
) {
	currentSnapshot := newNetworkSnapshot(id)
	knownStates := make(map[string]NetworkSnapshot)
	iter := uint64(0)

	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool, 1)
	snapshotTx := make(chan NetworkSnapshot, 1)
	snapshotRx := make(chan NetworkSnapshot)

	peerTxEnable <- true
	go peers.PeersTransmitter(peerPort, id, peerTxEnable)
	go peers.PeersReceiver(peerPort, peerUpdateCh)

	go bcast.BcastTransmitter(snapshotPort, snapshotTx)
	go bcast.BcastReceiver(snapshotPort, snapshotRx)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	startupTimer := time.NewTimer(4 * time.Second)
	defer startupTimer.Stop()

	readyToBroadcast := false
	wasSolo := false
	livePeerIDs := []string{id}

	// lostPeers is a persistent set of every peer ID that has ever been seen
	// to leave the network.
	lostPeers := make(map[string]bool)

	reconnectCooldown := 0

	sendSnapshot := func() {
		if !readyToBroadcast {
			return
		}
		if currentSnapshot.Elevators[id].Floor == -1 {
			return
		}
		select {
		case out.Snapshot <- currentSnapshot:
		default:
		}
	}

	enableBroadcast := func() {
		if !readyToBroadcast {
			readyToBroadcast = true
			peerTxEnable <- true

			elev := currentSnapshot.Elevators[id]
			settled := make([]RequestState, len(elev.CabRequests))
			copy(settled, elev.CabRequests)
			for floor, state := range settled {
				if state == UNKNOWN {
					settled[floor] = INACTIVE
				}
			}
			elev.CabRequests = settled
			currentSnapshot.Elevators[id] = elev

			var cabRequests []bool
			if recoveredElev, ok := currentSnapshot.Elevators[id]; ok {
				cabRequests = make([]bool, len(recoveredElev.CabRequests))
				for i, requestState := range recoveredElev.CabRequests {
					cabRequests[i] = RequestStateToBool(requestState)
				}
			}
			select {
			case out.ElevatorInitState <- elevatorcontroller.ElevatorInitState{
				CabRequests: cabRequests,
				DoorOpen:    currentSnapshot.Elevators[id].DoorOpen,
			}:
			default:
			}
		}
	}

	for {
		select {

		case <-startupTimer.C:
			enableBroadcast()

		case <-ticker.C:
			if !readyToBroadcast {
				break
			}

			if reconnectCooldown > 0 {
				reconnectCooldown--
				if reconnectCooldown == 0 {
					currentSnapshot.ReconnectedNode = false
				}
			}

			for peerID := range lostPeers {
				isLive := false
				for _, liveID := range livePeerIDs {
					if liveID == peerID {
						isLive = true
						break
					}
				}
				if isLive {
					continue
				}
				ownEntry := currentSnapshot.HallRequests[id]
				if ownEntry == nil {
					continue
				}
				mirrored := make([][2]RequestState, len(ownEntry))
				copy(mirrored, ownEntry)
				currentSnapshot.HallRequests[peerID] = mirrored
			}

			iter++
			currentSnapshot.Iter = iter
			knownStates[id] = currentSnapshot

			currentSnapshot = advanceCabToActive(currentSnapshot, livePeerIDs, knownStates)

			select {
			case snapshotTx <- currentSnapshot:
			default:
			}
			sendSnapshot()

		case elevatorState := <-in.ElevatorState:
			if elevatorState.Floor == -1 {
				break
			}
			currentCabRequests := currentSnapshot.Elevators[id].CabRequests
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState, currentCabRequests)

		case cabButton := <-in.CabButton:
			currentSnapshot = markCabButtonAsRequested(currentSnapshot, id, cabButton)

		case hallButton := <-in.HallButton:
			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)

		case servedRequests := <-in.ServedRequests:
			currentSnapshot = markRequestServed(currentSnapshot, id, servedRequests)

		case peerUpdate := <-peerUpdateCh:
			selfLost := false
			for _, lostID := range peerUpdate.Lost {
				if lostID == id {
					selfLost = true
					break
				}
			}

			for _, lostID := range peerUpdate.Lost {
				if lostID != id {
					lostPeers[lostID] = true
				}
			}

			if selfLost {
				currentSnapshot.ReconnectedNode = true
				livePeerIDs = []string{id}
				wasSolo = true

			} else if wasSolo && len(peerUpdate.Peers) > 1 {
				wasSolo = false

				reconnectCooldown = reconnectCooldownTicks

				ownHalls := make([][2]RequestState, elevatorcontroller.NumFloors)
				for i := range ownHalls {
					ownHalls[i] = [2]RequestState{UNKNOWN, UNKNOWN}
				}
				currentSnapshot.HallRequests[id] = ownHalls

				seen := map[string]bool{id: true}
				newLive := []string{id}
				for _, peer := range peerUpdate.Peers {
					if !seen[peer] {
						seen[peer] = true
						newLive = append(newLive, peer)
					}
				}
				livePeerIDs = newLive

			} else if len(peerUpdate.Peers) > 0 {
				seen := map[string]bool{id: true}
				newLive := []string{id}
				for _, peer := range peerUpdate.Peers {
					if !seen[peer] {
						seen[peer] = true
						newLive = append(newLive, peer)
					}
				}
				livePeerIDs = newLive
			} else {
				for _, lostID := range peerUpdate.Lost {
					livePeerIDs = removeFromSlice(livePeerIDs, lostID)
				}
			}

			select {
			case out.PeerUpdate <- peerUpdate:
			default:
			}

		case receivedSnapshot := <-snapshotRx:

			knownStates[receivedSnapshot.NodeID] = receivedSnapshot

			currentSnapshot = filteredMessage(currentSnapshot, receivedSnapshot, currentSnapshot.ReconnectedNode)

			currentSnapshot = propagateResetsToOwn(currentSnapshot, receivedSnapshot)
			currentSnapshot = adoptHallRequestsFromPeers(currentSnapshot)
			currentSnapshot = advanceHallToActive(currentSnapshot, livePeerIDs, knownStates)
			currentSnapshot = advanceCabToActive(currentSnapshot, livePeerIDs, knownStates)

			// Settle own UNKNOWN halls — any that survived the merge are
			// genuinely unknown to all peers, so INACTIVE is correct.
			ownHalls := currentSnapshot.HallRequests[id]
			for floor := range ownHalls {
				for btn := range ownHalls[floor] {
					if ownHalls[floor][btn] == UNKNOWN {
						ownHalls[floor][btn] = INACTIVE
					}
				}
			}
			currentSnapshot.HallRequests[id] = ownHalls

			if !readyToBroadcast && receivedSnapshot.NodeID != id {
				enableBroadcast()
			}
		}
	}
}

func hallButtonIndex(button elevatordriver.ButtonType) int {
	return int(button)
}

func markCabButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevatordriver.ButtonEvent) NetworkSnapshot {
	if _, ok := snapshot.Elevators[nodeID]; !ok {
		return snapshot
	}
	elevState := snapshot.Elevators[nodeID]
	if elevState.CabRequests[button.Floor] < REQUESTED {
		newCabReqs := make([]RequestState, len(elevState.CabRequests))
		copy(newCabReqs, elevState.CabRequests)
		newCabReqs[button.Floor] = REQUESTED
		elevState.CabRequests = newCabReqs
		snapshot.Elevators[nodeID] = elevState
	} else {
	}
	return snapshot
}

func markHallButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevatordriver.ButtonEvent) NetworkSnapshot {
	if _, ok := snapshot.HallRequests[nodeID]; !ok {
		return snapshot
	}
	buttonIndex := hallButtonIndex(button.Button)
	ownRequests := snapshot.HallRequests[nodeID]
	if ownRequests[button.Floor][buttonIndex] < REQUESTED {
		newReqs := make([][2]RequestState, len(ownRequests))
		copy(newReqs, ownRequests)
		newReqs[button.Floor][buttonIndex] = REQUESTED
		snapshot.HallRequests[nodeID] = newReqs
	}
	return snapshot
}

func removeFromSlice(slice []string, item string) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != item {
			out = append(out, v)
		}
	}
	return out
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

func newNetworkSnapshot(id string) NetworkSnapshot {
	ownCabRequests := make([]RequestState, elevatorcontroller.NumFloors)
	for i := range ownCabRequests {
		ownCabRequests[i] = UNKNOWN
	}
	ownHallRequests := make([][2]RequestState, elevatorcontroller.NumFloors)
	for i := range ownHallRequests {
		ownHallRequests[i] = [2]RequestState{UNKNOWN, UNKNOWN}
	}
	ownElevatorState := ElevatorState{
		Behaviour:   "idle",
		Floor:       -1,
		Direction:   "stop",
		CabRequests: ownCabRequests,
		DoorOpen:    false,
	}
	return NetworkSnapshot{
		NodeID: id,
		HallRequests: map[string][][2]RequestState{
			id: ownHallRequests,
		},
		Elevators: map[string]ElevatorState{
			id: ownElevatorState,
		},
		Iter:            0,
		ReconnectedNode: false,
	}
}

func resetHallRequest(snapshot NetworkSnapshot, id string, floor, buttonIndex int) NetworkSnapshot {
	ownRequests := snapshot.HallRequests[id]
	if ownRequests == nil || ownRequests[floor][buttonIndex] != ACTIVE {
		return snapshot
	}
	newRequests := make([][2]RequestState, len(ownRequests))
	copy(newRequests, ownRequests)
	newRequests[floor][buttonIndex] = INACTIVE
	snapshot.HallRequests[id] = newRequests

	for peerID, peerRequests := range snapshot.HallRequests {
		if peerID == id || peerRequests == nil {
			continue
		}
		if peerRequests[floor][buttonIndex] == ACTIVE {
			newPeerReqs := make([][2]RequestState, len(peerRequests))
			copy(newPeerReqs, peerRequests)
			newPeerReqs[floor][buttonIndex] = INACTIVE
			snapshot.HallRequests[peerID] = newPeerReqs
		}
	}
	return snapshot
}

func resetCabRequest(snapshot NetworkSnapshot, id string, floor int) NetworkSnapshot {
	elev, ok := snapshot.Elevators[id]
	if !ok || elev.CabRequests[floor] != ACTIVE {
		return snapshot
	}
	newCabReqs := make([]RequestState, len(elev.CabRequests))
	copy(newCabReqs, elev.CabRequests)
	newCabReqs[floor] = INACTIVE
	elev.CabRequests = newCabReqs
	snapshot.Elevators[id] = elev
	return snapshot
}

func markRequestServed(snapshot NetworkSnapshot, id string, served elevatordriver.ButtonEvent) NetworkSnapshot {
	if served.Button == elevatordriver.BT_Cab {
		return resetCabRequest(snapshot, id, served.Floor)
	}
	return resetHallRequest(snapshot, id, served.Floor, int(served.Button))
}
