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
	peerPort     = 15657
	snapshotPort = 15667
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

	// log.Log("Network %s: peerPort=%d snapPort=%d", id, peerPort, snapshotPort)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	startupTimer := time.NewTimer(4 * time.Second)
	defer startupTimer.Stop()

	readyToBroadcast := false
	//isSolo := false
	//receivedSnapshotFromPeer := false
	//peerConfirmed := false
	livePeerIDs := []string{id}

	sendSnapshot := func() {
		if !readyToBroadcast {
			return
		}
		if currentSnapshot.Elevators[id].Floor == -1 {
			return
		}
		log.Log("[SNAPSHOT->MGR] sending snapshot to manager: hallRequests=%v floor=%d", currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id].Floor)
		select {
		case out.Snapshot <- currentSnapshot:
		default:
		}
	}

	enableBroadcast := func() {
		if !readyToBroadcast {
			readyToBroadcast = true
			peerTxEnable <- true

			// Settle any unrecovered cab requests to INACTIVE now that startup is complete
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

			// Settle any unrecovered hall requests to INACTIVE now that startup is complete
			// FilteredMessage has already run if peers were present, so only genuinely
			// unknown floors (no peer knowledge) remain as UNKNOWN here
			ownHallRequests := currentSnapshot.HallRequests[id]
			if ownHallRequests != nil {
				for floor := range ownHallRequests {
					for btn := range ownHallRequests[floor] {
						if ownHallRequests[floor][btn] == UNKNOWN {
							ownHallRequests[floor][btn] = INACTIVE
						}
					}
				}
				currentSnapshot.HallRequests[id] = ownHallRequests
				log.Log("[INIT] settled remaining UNKNOWN hall requests to INACTIVE")
			}

			var cabRequests []bool
			if recoveredElev, ok := currentSnapshot.Elevators[id]; ok {
				cabRequests = make([]bool, len(recoveredElev.CabRequests))
				for i, requestState := range recoveredElev.CabRequests {
					cabRequests[i] = RequestStateToBool(requestState)
				}
			}
			log.Log("[INIT] enableBroadcast fired, sending InitCabRequests: %v to elevator", cabRequests)
			select {
			case out.InitCabRequests <- cabRequests:
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
			iter++
			currentSnapshot.Iter = iter
			knownStates[id] = currentSnapshot
			// log.Log("[TX] iter=%d hallRequests=%v elevators=%v", iter, currentSnapshot.HallRequests[id], currentSnapshot.Elevators[id])

			select {
			case snapshotTx <- currentSnapshot:
			default:
			}
			sendSnapshot()

		case elevatorState := <-in.ElevatorState:

			if elevatorState.Floor == -1 {
				log.Log("[GUARD] dropping elevator state with floor=-1, not updating snapshot")
				break
			}
			currentCabRequests := currentSnapshot.Elevators[id].CabRequests
			currentSnapshot.Elevators[id] = LocalElevatorToElevatorState(elevatorState, currentCabRequests)
			log.Log("[ELEV STATE] updated snapshot floor=%d dir=%s beh=%s", elevatorState.Floor, elevatorcontroller.DirnToString(elevatorState.Direction), elevatorState.Behaviour.String())

		case cabButton := <-in.CabButton:
			log.Log("[CAB BTN] received button event %v", cabButton)
			currentSnapshot = markCabButtonAsRequested(currentSnapshot, id, cabButton)
			// sendSnapshot()

		case hallButton := <-in.HallButton:

			log.Log("[HALL BTN] received button event %v", hallButton)

			currentSnapshot = markHallButtonAsRequested(currentSnapshot, id, hallButton)
			//currentSnapshot = AdvanceHallToActive(currentSnapshot, livePeerIDs, knownStates)

			//if readyToBroadcast {
			//	iter++
			//	currentSnapshot.Iter = iter
			//	knownStates[id] = currentSnapshot
			//	forceSend(snapshotTx, currentSnapshot)
			//}

			//sendSnapshot()

		case servedRequests := <-in.ServedRequests:
			log.Log("[SERVED REQ] network received served requests: %v", servedRequests)
			currentSnapshot = markRequestServed(currentSnapshot, id, servedRequests)

		case peerUpdate := <-peerUpdateCh:
			if len(peerUpdate.Peers) > 0 {
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

			log.Log("[SNAPSHOT RX] received snapshot iter=%d from node %s with %d elevators and hall requests: %v", receivedSnapshot.Iter, receivedSnapshot.NodeID, len(receivedSnapshot.Elevators), receivedSnapshot.HallRequests)

			knownStates[receivedSnapshot.NodeID] = receivedSnapshot
			currentSnapshot = FilteredMessage(currentSnapshot, receivedSnapshot)
			currentSnapshot = propagateResetsToOwn(currentSnapshot, receivedSnapshot)
			currentSnapshot = adoptHallRequestsFromPeers(currentSnapshot)
			currentSnapshot = AdvanceHallToActive(currentSnapshot, livePeerIDs, knownStates)
			currentSnapshot = AdvanceCabToActive(currentSnapshot, livePeerIDs, knownStates)

			if !readyToBroadcast && receivedSnapshot.NodeID != id {
				enableBroadcast()
			}

			//enableBroadcast()
			//sendSnapshot()

		}
	}
}

//func forceSend(ch chan NetworkSnapshot, snapshot NetworkSnapshot) {
//	select {
//	case ch <- snapshot:
//	default:
//		select {
//		case <-ch:
//		default:
//		}
//		select {
//		case ch <- snapshot:
//		default:
//		}
//	}
//}

func hallButtonIndex(button elevio.ButtonType) int {
	return int(button)
}

func markCabButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevio.ButtonEvent) NetworkSnapshot {
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

func markHallButtonAsRequested(snapshot NetworkSnapshot, nodeID string, button elevio.ButtonEvent) NetworkSnapshot {

	if _, ok := snapshot.HallRequests[nodeID]; !ok {
		log.Log("[HALL BTN] ERROR: nodeID=%s not in HallRequests keys=%v", nodeID, snapshot.HallRequests)
		return snapshot
	}
	// log.Log("markHallButtonAsRequested: nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)

	buttonIndex := hallButtonIndex(button.Button)

	if _, ok := snapshot.HallRequests[nodeID]; !ok {
		// log.Log("markHallButtonAsRequested: nodeID=%s not found in HallRequests, returning early", nodeID)
		return snapshot
	}

	ownRequests := snapshot.HallRequests[nodeID]
	if ownRequests[button.Floor][buttonIndex] < REQUESTED {
		newReqs := make([][2]RequestState, len(ownRequests))
		copy(newReqs, ownRequests)

		newReqs[button.Floor][buttonIndex] = REQUESTED
		snapshot.HallRequests[nodeID] = newReqs

		log.Log("[HALL BTN] marked as REQUESTED for nodeID=%s floor=%d btn=%d", nodeID, button.Floor, button.Button)
	} else {
		log.Log("[HALL BTN] floor=%d btn=%d already at state=%d, skipping", button.Floor, button.Button, ownRequests[button.Floor][buttonIndex])
	}
	return snapshot
}

// removeFromSlice returns a new slice with all occurrences of item removed.
func removeFromSlice(slice []string, item string) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != item {
			out = append(out, v)
		}
	}
	return out
}

//func isAloneOnNetwork(peerUpdate peers.PeerUpdate, id string) bool {
//	for _, peer := range peerUpdate.Peers {
//		if peer != id {
//			return false
//		}
//	}
//	return true
//}

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
		Iter: 0,
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

	// Clear peer copies immediately so HRA doesn't re-assign before
	// broadcast propagation completes
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

func markRequestServed(snapshot NetworkSnapshot, id string, served elevio.ButtonEvent) NetworkSnapshot {

	log.Log("[REQUEST SERVED] nodeID=%s floor=%d btn=%d", id, served.Floor, served.Button)

	if served.Button == elevio.BT_Cab {
		return resetCabRequest(snapshot, id, served.Floor)
	}
	return resetHallRequest(snapshot, id, served.Floor, int(served.Button))
}
