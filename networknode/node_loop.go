package networknode

import (
	log "Heislab/Log"
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
	// stays true after a node re-joins the network.  100 ticks = 1 second gives
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

	log.Log("Network %s: peerPort=%d snapshotPort=%d", id, peerPort, snapshotPort)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	startupTimer := time.NewTimer(4 * time.Second)
	defer startupTimer.Stop()

	readyToBroadcast := false
	wasSolo := false
	livePeerIDs := []string{id}

	// lostPeers is a persistent set of every peer ID that has ever been seen
	// to leave the network.  It is never cleared — once a peer has been lost
	// it is remembered for the lifetime of this node so we can detect when it
	// reconnects and treat it appropriately.
	lostPeers := make(map[string]bool)

	// reconnectCooldown counts down from reconnectCooldownTicks to 0 after
	// this node itself reconnects.  While non-zero, currentSnapshot.ReconnectedNode
	// is true.
	//
	// Critically, the flag is set at self-lost time (not reconnect time), so
	// every snapshot broadcast during the solo window already carries it.
	// There is no race between the ticker firing and the peerUpdate reconnect
	// branch: by the time we're back on the network the flag is already set.
	reconnectCooldown := 0

	sendSnapshot := func() {
		if !readyToBroadcast {
			return
		}
		if currentSnapshot.Elevators[id].Floor == -1 {
			return
		}
		log.Log("[SNAPSHOT->MGR] sending snapshot to coordinator: hallRequests=%v floor=%d, Service Status: %t",
			currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id].Floor, currentSnapshot.Elevators[id].IsOutOfService)
		select {
		case out.Snapshot <- currentSnapshot:
		default:
		}
	}

	enableBroadcast := func() {
		if !readyToBroadcast {
			readyToBroadcast = true
			peerTxEnable <- true

			// Settle cabs only — FSM needs a definite bool on startup
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
			log.Log("[INIT] settled remaining UNKNOWN cab requests to INACTIVE")

			// Halls deliberately NOT settled here — let merger resolve them
			// from peer snapshots naturally over the next few ticks

			var cabRequests []bool
			if recoveredElev, ok := currentSnapshot.Elevators[id]; ok {
				cabRequests = make([]bool, len(recoveredElev.CabRequests))
				for i, requestState := range recoveredElev.CabRequests {
					cabRequests[i] = RequestStateToBool(requestState)
				}
			}
			log.Log("[INIT] enableBroadcast fired, sending elevator init state: %v to elevator", cabRequests)
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
			log.Log("[INIT] startup timer fired, no peers heard from, enabling broadcast solo")
			enableBroadcast()

		case <-ticker.C:
			if !readyToBroadcast {
				break
			}

			// Tick down the reconnect cooldown.  When it reaches zero the node
			// has been back on the network long enough that peers will have
			// converged on the correct hall states, and we can stop signalling
			// ReconnectedNode.
			if reconnectCooldown > 0 {
				reconnectCooldown--
				if reconnectCooldown == 0 {
					currentSnapshot.ReconnectedNode = false
					log.Log("[RECONNECT] cooldown expired, clearing ReconnectedNode flag")
				}
			}

			// While a peer is known-lost, mirror our own hall entry into their
			// slot in currentSnapshot.  When that peer reconnects it receives
			// our snapshot, sees our ACTIVE entries under its own nodeID, and
			// adopts them because the isUnResettingState guard is lifted for
			// nodes that are in their reconnect window (ReconnectedNode=true).
			// This ensures unserved requests we observed while the peer was
			// away are not silently dropped.
			for peerID := range lostPeers {
				isLive := false
				for _, liveID := range livePeerIDs {
					if liveID == peerID {
						isLive = true
						break
					}
				}
				if isLive {
					// Peer is back on the network — stop overwriting its slot
					// and let the normal merge algorithm take over.
					continue
				}
				ownEntry := currentSnapshot.HallRequests[id]
				if ownEntry == nil {
					continue
				}
				mirrored := make([][2]RequestState, len(ownEntry))
				copy(mirrored, ownEntry)
				currentSnapshot.HallRequests[peerID] = mirrored
				log.Log("[LOST MIRROR] mirroring own halls to lost peer=%s entry", peerID)
			}

			iter++
			currentSnapshot.Iter = iter
			knownStates[id] = currentSnapshot
			log.Log("[TX] iter=%d hallRequests=%v elevators=%v", iter, currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id])

			for knownStates, id := range currentSnapshot.Elevators {
				log.Log("[STATUS] perspective %v sees elevator %v with Service Status: %t",
					id, knownStates, currentSnapshot.Elevators[string(knownStates)].IsOutOfService)
			}

			// Solo mode: advance cabs without peer consensus.
			// Hall advancement is intentionally omitted — halls stay REQUESTED
			// during solo to avoid propagateResetsToOwn ambiguity on reconnect.
			currentSnapshot = AdvanceCabToActive(currentSnapshot, livePeerIDs, knownStates)

			select {
			case snapshotTx <- currentSnapshot:
			default:
			}
			sendSnapshot()

		case elevatorState := <-in.ElevatorState:
			log.Log("[ELEV STATE] networknode received Out Of Service status of elevator: %t", elevatorState.IsOutOfService)
			if elevatorState.Floor == -1 {
				log.Log("[GUARD] dropping elevator state with floor=-1, not updating snapshot")
				break
			}
			currentCabRequests := currentSnapshot.Elevators[id].CabRequests
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState, currentCabRequests)
			log.Log("[ELEV STATE] updated snapshot floor=%d dir=%s beh=%s",
				elevatorState.Floor,
				elevatorcontroller.DirnToString(elevatorState.Direction),
				elevatorState.Behaviour.String())

		case cabButton := <-in.CabButton:
			log.Log("[CAB BTN] received button event %v", cabButton)
			currentSnapshot = markCabButtonAsRequested(currentSnapshot, id, cabButton)

		case hallButton := <-in.HallButton:
			log.Log("[HALL BTN] received button event %v", hallButton)
			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)

		case servedRequests := <-in.ServedRequests:
			log.Log("[SERVED REQ] network received served requests: %v", servedRequests)
			currentSnapshot = markRequestServed(currentSnapshot, id, servedRequests)

		case peerUpdate := <-peerUpdateCh:
			selfLost := false
			for _, lostID := range peerUpdate.Lost {
				if lostID == id {
					selfLost = true
					break
				}
			}

			// Record every newly-lost non-self peer in the persistent set.
			for _, lostID := range peerUpdate.Lost {
				if lostID != id {
					log.Log("[LOST PEERS] remembering lost peer=%s", lostID)
					lostPeers[lostID] = true
				}
			}

			if selfLost {
				// Set ReconnectedNode immediately at disconnect time — NOT at
				// reconnect time.  This is the key invariant: every snapshot we
				// broadcast from this point forward carries the flag, so peers
				// will never misread our offline INACTIVEs as fresh reset signals
				// regardless of timing.
				log.Log("[PEER] self lost from network, entering solo mode, setting ReconnectedNode=true")
				currentSnapshot.ReconnectedNode = true
				livePeerIDs = []string{id}
				wasSolo = true

			} else if wasSolo && len(peerUpdate.Peers) > 1 {
				log.Log("[PEER] reconnecting after solo, resetting own halls to UNKNOWN for peer merge, starting reconnect cooldown=%d ticks", reconnectCooldownTicks)
				wasSolo = false

				// Start the cooldown.  ReconnectedNode is already true from
				// when selfLost fired — we just need to schedule its expiry.
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
			log.Log("[SNAPSHOT RX] received snapshot iter=%d from node %s with %d elevators and hall requests: %v",
				receivedSnapshot.Iter, receivedSnapshot.NodeID, len(receivedSnapshot.Elevators), receivedSnapshot.HallRequests)
			log.Log("[SNAPSHOT RX] service status of received elevators:")
			for nodeID, elevState := range receivedSnapshot.Elevators {
				log.Log("[SNAPSHOT RX]   nodeID=%s isOutOfService=%t", nodeID, elevState.IsOutOfService)
			}
			if receivedSnapshot.ReconnectedNode {
				log.Log("[SNAPSHOT RX] sender=%s is in reconnect window (ReconnectedNode=true), propagateResetsToOwn will be skipped", receivedSnapshot.NodeID)
			}

			knownStates[receivedSnapshot.NodeID] = receivedSnapshot

			// Pass our own ReconnectedNode flag into FilteredMessage so the
			// merge layer can lift the isUnResettingState guard on our own
			// entry — allowing us to adopt the network's ACTIVE perspective
			// of what we still owe after our offline stint.
			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot, currentSnapshot.ReconnectedNode)

			// propagateResetsToOwn checks received.ReconnectedNode internally
			// and returns early if set — stale offline INACTIVEs must never
			// clear our ACTIVE hall requests.
			currentSnapshot = propagateResetsToOwn(currentSnapshot, receivedSnapshot)
			currentSnapshot = adoptHallRequestsFromPeers(currentSnapshot)
			currentSnapshot = AdvanceHallToActive(currentSnapshot, livePeerIDs, knownStates)
			currentSnapshot = AdvanceCabToActive(currentSnapshot, livePeerIDs, knownStates)

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
	log.Log("[CAB BTN] marking as REQUESTED nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)
	if _, ok := snapshot.Elevators[nodeID]; !ok {
		log.Log("[CAB BTN] ERROR: nodeID=%s not in Elevators keys=%v", nodeID, snapshot.Elevators)
		return snapshot
	}
	elevState := snapshot.Elevators[nodeID]
	if elevState.CabRequests[button.Floor] < REQUESTED {
		newCabReqs := make([]RequestState, len(elevState.CabRequests))
		copy(newCabReqs, elevState.CabRequests)
		newCabReqs[button.Floor] = REQUESTED
		elevState.CabRequests = newCabReqs
		snapshot.Elevators[nodeID] = elevState
		log.Log("[CAB BTN] marked as REQUESTED for nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)
	} else {
		log.Log("[CAB BTN] floor=%d btn=%d already at state=%d, skipping", button.Floor, button.Button, elevState.CabRequests[button.Floor])
	}
	return snapshot
}

func markHallButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevatordriver.ButtonEvent) NetworkSnapshot {
	if _, ok := snapshot.HallRequests[nodeID]; !ok {
		log.Log("[HALL BTN] ERROR: nodeID=%s not in HallRequests keys=%v", nodeID, snapshot.HallRequests)
		return snapshot
	}
	buttonIndex := hallButtonIndex(button.Button)
	ownRequests := snapshot.HallRequests[nodeID]
	if ownRequests[button.Floor][buttonIndex] < REQUESTED {
		newReqs := make([][2]RequestState, len(ownRequests))
		copy(newReqs, ownRequests)
		newReqs[button.Floor][buttonIndex] = REQUESTED
		snapshot.HallRequests[nodeID] = newReqs
		log.Log("[HALL BTN] marked as REQUESTED for nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)
	} else {
		log.Log("[HALL BTN] floor=%d btn=%d already at state=%d, skipping",
			button.Floor, button.Button, ownRequests[button.Floor][buttonIndex])
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
	log.Log("[HALL RESET] mark as INACTIVE nodeID=%s floor=%d btn=%d", id, floor, buttonIndex)
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
			log.Log("[HALL RESET] also clearing peer copy for nodeID=%s floor=%d btn=%d", peerID, floor, buttonIndex)
		}
	}
	return snapshot
}

func resetCabRequest(snapshot NetworkSnapshot, id string, floor int) NetworkSnapshot {
	log.Log("[CAB RESET] mark as INACTIVE nodeID=%s floor=%d", id, floor)
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
	log.Log("[REQUEST SERVED] nodeID=%s floor=%d btn=%d", id, served.Floor, served.Button)
	if served.Button == elevatordriver.BT_Cab {
		return resetCabRequest(snapshot, id, served.Floor)
	}
	return resetHallRequest(snapshot, id, served.Floor, int(served.Button))
}
