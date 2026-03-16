package networknode

import (
	log "Heislab/Log"
)

// filter_messages.go implements the cyclic counter algorithm for merging
// distributed elevator request states across network nodes.
//
// Each request (cab or hall) is represented as a RequestState rather than
// a plain bool, allowing nodes to resolve conflicts without a central
// authority. The algorithm is described in the Anders' TTK4145 Practice lecture:
//
//   TTK4145 Real-Time Programming – Lecture 3
//   "Ex 5: Cyclic Counter" (timestamp 1:16:00)
//   [https://youtu.be/0VRZ02npbTM?t=4560](https://youtu.be/0VRZ02npbTM?t=4560)
//
// Algorithm summary:
//   - Discard any received value <= local (counts upward)
//   - Except: accept Inactive if local is Active (allows reset)
//   - Except: reject Active if local is Inactive (prevents un-resetting)
//   - Requires n >= 3 distinct states (satisfied by Unknown/Inactive/Requested/Active)

func shouldResetState(local, received RequestState) bool {
	return local == ACTIVE && received == INACTIVE
}

func isUnResettingState(local, received RequestState) bool {
	return local == INACTIVE && received == ACTIVE
}

func shouldAdvanceState(local, received RequestState) bool {
	if shouldResetState(local, received) {
		return true
	}
	return received > local
}

// returnValidState returns the correct state after comparing a local and a
// received value using the cyclic counter algorithm.
func returnValidState(local, received RequestState) RequestState {
	if shouldAdvanceState(local, received) {
		return received
	}
	return local
}

// mergeCabRequests merges a received cab request array into the local one.
// Cab requests are node-local and should only be merged from the same node.
func mergeCabRequests(local, received []RequestState, localID, receivedID, ownID string) []RequestState {
	size := len(received)
	if len(local) > size {
		size = len(local)
	}

	merged := make([]RequestState, size)
	for i := range merged {
		merged[i] = INACTIVE // peers default to INACTIVE, not UNKNOWN
	}
	copy(merged, local)

	// Allow merge if sender owns this entry, OR if this is our own entry
	// being recovered from a peer's snapshot (restart recovery).
	if localID != receivedID && localID != ownID {
		return merged
	}

	for floor := range local {
		if localID == ownID && isUnResettingState(local[floor], received[floor]) {
			merged[floor] = local[floor]
		} else {
			merged[floor] = returnValidState(local[floor], received[floor])
		}
	}
	return merged
}

func copyHallRequests(src map[string][][2]RequestState) map[string][][2]RequestState {
	dst := make(map[string][][2]RequestState, len(src))
	for nodeID, reqs := range src {
		// Deep copy the slice of [2]RequestState
		innerCopy := make([][2]RequestState, len(reqs))
		copy(innerCopy, reqs)
		dst[nodeID] = innerCopy
	}
	return dst
}

func mergePeerEntry(local, received [][2]RequestState, isOwnEntry bool) [][2]RequestState {
	merged := make([][2]RequestState, len(received))
	for floor := range received {
		for btn := range received[floor] {
			localState := UNKNOWN
			if local != nil {
				localState = local[floor][btn]
			}
			if isOwnEntry && shouldResetState(localState, received[floor][btn]) {
				merged[floor][btn] = localState
			} else if isUnResettingState(localState, received[floor][btn]) {
				merged[floor][btn] = localState
			} else {
				merged[floor][btn] = returnValidState(localState, received[floor][btn])
			}
		}
	}
	return merged
}

func mergeHallRequests(local, received map[string][][2]RequestState, localID string) map[string][][2]RequestState {
	merged := copyHallRequests(local)

	for nodeID, receivedEntry := range received {
		// Correct: protect my own entry only
		isOwnEntry := (nodeID == localID)

		merged[nodeID] = mergePeerEntry(local[nodeID], receivedEntry, isOwnEntry)
	}
	return merged
}

func copyElevators(src map[string]ElevatorState) map[string]ElevatorState {
	dst := make(map[string]ElevatorState, len(src))
	for nodeID, state := range src {
		dst[nodeID] = state
	}
	return dst
}

func mergeElevators(local, received NetworkSnapshot) map[string]ElevatorState {
	merged := copyElevators(local.Elevators)

	log.Log("mergeElevators: received elevators=%v", received.Elevators)

	for nodeID, receivedState := range received.Elevators {
		localState, exists := local.Elevators[nodeID]

		if receivedState.Floor == -1 {
			log.Log("[mergeElevators] skipping uninitialized floor=-1 state from peer=%s", nodeID)
			continue
		}

		var currentCabs []RequestState
		if exists {
			currentCabs = localState.CabRequests
		}
		log.Log("mergeElevators: merging cabRequests for nodeID=%s, %v (exists=%v)", nodeID, currentCabs, exists)
		mergedCabs := mergeCabRequests(
			currentCabs,
			receivedState.CabRequests,
			nodeID,
			received.NodeID,
			local.NodeID,
		)

		// Motion state is ground truth from sensors — only the owner can update it.
		// Peers may only contribute cab request recovery, nothing else.
		if nodeID == received.NodeID {
			merged[nodeID] = ElevatorState{
				Behaviour:   receivedState.Behaviour,
				Floor:       receivedState.Floor,
				Direction:   receivedState.Direction,
				DoorOpen:    receivedState.DoorOpen,
				CabRequests: mergedCabs,
			}
		} else if exists {
			// Accept the peer's DoorOpen during startup recovery only.
			// localState.DoorOpen initialises to false in newNetworkSnapshot,
			// so without this the door state is always lost on restart.
			recoveredDoorOpen := localState.DoorOpen
			if nodeID == local.NodeID && localState.Floor == -1 {
				recoveredDoorOpen = receivedState.DoorOpen
				log.Log("[mergeElevators] recovering DoorOpen=%v for own entry from peer=%s",
					recoveredDoorOpen, received.NodeID)
			}
			merged[nodeID] = ElevatorState{
				Behaviour:   localState.Behaviour,
				Floor:       localState.Floor,
				Direction:   localState.Direction,
				DoorOpen:    recoveredDoorOpen,
				CabRequests: mergedCabs,
			}
		}
	}
	return merged
}

func FilteredMessage(local, received NetworkSnapshot) NetworkSnapshot {
	log.Log("FilteredMessage: merging snapshot from %s (iter %d)", received.NodeID, received.Iter)
	merged := local
	merged.Elevators = mergeElevators(local, received)
	merged.HallRequests = mergeHallRequests(local.HallRequests, received.HallRequests, local.NodeID)
	log.Log("FilteredMessage: result hallRequests=%v", merged.HallRequests)
	log.Log("FilteredMessage: result elevators=%v", merged.Elevators)
	return merged
}

// shouldAdoptHallRequest returns true when own entry for floorIndex/buttonIndex
// is INACTIVE and at least one peer has already reached REQUESTED.
// This is the adoption predicate: case B of the INACTIVE→REQUESTED transition.
//func shouldAdoptHallRequest(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
//	ownRequest := snapshot.HallRequests[snapshot.NodeID][floorIndex][buttonIndex]
//	return (ownRequest == UNKNOWN || ownRequest == INACTIVE) && isHallRequestKnownByAnyPeer(snapshot, floorIndex, buttonIndex)
//}

func adoptHallRequestsFromPeers(snapshot NetworkSnapshot) NetworkSnapshot {
	ownRequests := snapshot.HallRequests[snapshot.NodeID]
	if ownRequests == nil {
		return snapshot
	}
	for floor := range ownRequests {
		for btn := range ownRequests[floor] {
			if ownRequests[floor][btn] != INACTIVE {
				continue
			}
			for peerID, peerRequests := range snapshot.HallRequests {
				if peerID == snapshot.NodeID || peerRequests == nil {
					continue
				}
				if peerRequests[floor][btn] == REQUESTED {
					log.Log("adoptHallRequestsFromPeers: floor=%d btn=%d INACTIVE→REQUESTED (from peer=%s)", floor, btn, peerID)
					ownRequests[floor][btn] = REQUESTED
					break
				}
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}

// propagateResetsToOwn clears own ACTIVE entries to INACTIVE when the sender
// of the received snapshot reports INACTIVE for that floor/button in its own entry.
// This propagates served-request resets from the elevator that served them.
func propagateResetsToOwn(snapshot, received NetworkSnapshot) NetworkSnapshot {
	if received.NodeID == snapshot.NodeID {
		return snapshot
	}
	own := snapshot.HallRequests[snapshot.NodeID]
	if own == nil {
		return snapshot
	}
	senderEntry := received.HallRequests[received.NodeID]
	if senderEntry == nil {
		return snapshot
	}
	changed := false
	for floor := range own {
		for button := 0; button < 2; button++ {
			if own[floor][button] == ACTIVE && senderEntry[floor][button] == INACTIVE {
				log.Log("propagateResetsToOwn: floor=%d btn=%d ACTIVE→INACTIVE (reset by %s)", floor, button, received.NodeID)
				own[floor][button] = INACTIVE
				changed = true
			}
		}
	}
	if changed {
		snapshot.HallRequests[snapshot.NodeID] = own
	}
	return snapshot
}

//func isHallRequestKnownByAnyPeer(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
//	for peerID, peerRequests := range snapshot.HallRequests {
//		if peerID == snapshot.NodeID {
//			continue
//		}
//		if peerRequests != nil && peerRequests[floorIndex][buttonIndex] >= REQUESTED {
//			return true
//		}
//	}
//	return false
//}

func allLivePeersKnowRequestedCabs(
	snapshot NetworkSnapshot,
	livePeerIDs []string,
	knownStates map[string]NetworkSnapshot,
	floor int,
) bool {
	consensusPeers := make([]string, 0)
	for _, peerID := range livePeerIDs {
		if peerID == snapshot.NodeID {
			continue
		}
		if _, known := knownStates[peerID]; known {
			consensusPeers = append(consensusPeers, peerID)
		}
	}
	log.Log("allLivePeersKnowRequestedCabs: (floor=%d): livePeerIDs=%v consensusPeers=%v ownStatus=%d",
		floor, livePeerIDs, consensusPeers, snapshot.Elevators[snapshot.NodeID].CabRequests[floor])

	if len(consensusPeers) == 0 {
		soloMode := len(livePeerIDs) <= 1
		log.Log("allLivePeersKnowRequestedCabs : no consensusPeers, soloMode=%v → %v", soloMode, soloMode)
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerElev, ok := knownStates[peerID].Elevators[snapshot.NodeID]
		if !ok {
			log.Log("allLivePeersKnowRequestedCabs : peer=%s has no entry for nodeID=%s", peerID, snapshot.NodeID)
			return false
		}
		peerStatus := peerElev.CabRequests[floor]
		log.Log("allLivePeersKnowRequestedCabs : peer=%s status=%d (need >= REQUESTED=%d)", peerID, peerStatus, REQUESTED)
		if peerStatus < REQUESTED {
			log.Log("allLivePeersKnowRequestedCabs : peer %s vetoes (status %d)", peerID, peerStatus)
			return false
		}
	}
	log.Log("allLivePeersKnowRequestedCabs : all consensusPeers >= REQUESTED → ACTIVE")
	return true
}

func allLivePeersKnowRequestedHall(
	snapshot NetworkSnapshot,
	livePeerIDs []string,
	knownStates map[string]NetworkSnapshot,
	floor, button int,
) bool {
	consensusPeers := make([]string, 0)
	for _, peerID := range livePeerIDs {
		if peerID == snapshot.NodeID {
			continue
		}
		if _, known := knownStates[peerID]; known {
			consensusPeers = append(consensusPeers, peerID)
		}
	}

	log.Log("allLivePeersKnowRequestedHall(floor=%d btn=%d): livePeerIDs=%v consensusPeers=%v ownStatus=%d",
		floor, button, livePeerIDs, consensusPeers, snapshot.HallRequests[snapshot.NodeID][floor][button])

	if len(consensusPeers) == 0 {
		soloMode := len(livePeerIDs) <= 1
		log.Log("allLivePeersKnowRequestedHall: no consensusPeers, soloMode=%v → %v", soloMode, soloMode)
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerEntry, ok := knownStates[peerID].HallRequests[peerID]
		if !ok {
			log.Log("allLivePeersKnowRequestedHall: peer=%s has no own entry", peerID)
			return false
		}
		peerStatus := peerEntry[floor][button]
		log.Log("allLivePeersKnowRequestedHall: peer=%s status=%d (need >= REQUESTED=%d)", peerID, peerStatus, REQUESTED)
		if peerStatus < REQUESTED {
			log.Log("allLivePeersKnowRequestedHall: peer %s vetoes (status %d)", peerID, peerStatus)
			return false
		}
	}

	log.Log("allLivePeersKnowRequestedHall: all consensusPeers >= REQUESTED → ACTIVE")
	return true
}

func AdvanceCabToActive(
	snapshot NetworkSnapshot,
	livePeerIDs []string,
	knownStates map[string]NetworkSnapshot,
) NetworkSnapshot {
	elev, ok := snapshot.Elevators[snapshot.NodeID]
	if !ok {
		return snapshot
	}
	for floor, state := range elev.CabRequests {
		if state == REQUESTED {
			if allLivePeersKnowRequestedCabs(snapshot, livePeerIDs, knownStates, floor) {
				log.Log("AdvanceCabToActive: floor=%d REQUESTED→ACTIVE peers=%v", floor, livePeerIDs)
				elev.CabRequests[floor] = ACTIVE

			} else {
				log.Log("AdvanceCabToActive: floor=%d REQUESTED but peers not ready peers=%v", floor, livePeerIDs)
			}
		}
	}
	snapshot.Elevators[snapshot.NodeID] = elev
	return snapshot
}

// AdvanceToActive promotes own hall requests from REQUESTED to ACTIVE for every
// floor/button where all live peers have reached at least REQUESTED.
// Requiring full peer consensus before going ACTIVE ensures propagateResetsToOwn
// is never triggered by a peer that is simply unaware of the request.
// filter_message.go
func AdvanceHallToActive(
	snapshot NetworkSnapshot,
	livePeerIDs []string,
	knownStates map[string]NetworkSnapshot,
) NetworkSnapshot {
	ownRequests := snapshot.HallRequests[snapshot.NodeID]
	if ownRequests == nil {
		return snapshot
	}
	for f := range ownRequests {
		for b := 0; b < 2; b++ {
			if ownRequests[f][b] == REQUESTED {
				if allLivePeersKnowRequestedHall(snapshot, livePeerIDs, knownStates, f, b) {
					log.Log("AdvanceToActive: floor=%d btn=%d REQUESTED→ACTIVE peers=%v", f, b, livePeerIDs)
					ownRequests[f][b] = ACTIVE
				} else {
					log.Log("AdvanceToActive: floor=%d btn=%d REQUESTED but peers not ready peers=%v", f, b, livePeerIDs)
				}
			}
		}

	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}
