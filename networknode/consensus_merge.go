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
//   https://youtu.be/0VRZ02npbTM?t=4560
//
// Algorithm summary:
//   - Discard any received value <= local (counts upward)
//   - Except: accept Inactive if local is Active (allows reset)
//   - Except: reject Active if local is Inactive (prevents un-resetting)
//   - Requires n >= 3 distinct states (satisfied by Unknown/Inactive/Requested/Active)
//
// ReconnectedNode extensions:
//
//   When received.ReconnectedNode is true (sender just came back from solo):
//     - propagateResetsToOwn skips the sender entirely — its INACTIVEs are
//       stale offline serves, not fresh reset signals.
//     - mergePeerEntry does not allow the sender's INACTIVE to overwrite our
//       ACTIVE view of the sender (senderIsReconnecting guard).
//
//   When localIsReconnecting is true (we ourselves are in the reconnect window):
//     - mergePeerEntry lifts the isUnResettingState guard on our own entry so
//       the network's ACTIVE view of what we owe can overwrite our INACTIVE.
//     - mergePeerEntry also skips the own-entry ACTIVE→INACTIVE reset protection
//       for the same reason: we want to adopt the network's current picture.

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
		} else if localID == ownID && shouldResetState(local[floor], received[floor]) && receivedID != ownID {
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
		innerCopy := make([][2]RequestState, len(reqs))
		copy(innerCopy, reqs)
		dst[nodeID] = innerCopy
	}
	return dst
}

// mergePeerEntry merges one nodeID's hall request entry from a received snapshot
// into the local copy.
//
// isOwnEntry         — true when nodeID == local.NodeID (protecting our own entry)
// localIsReconnecting — true when we are in our own reconnect cooldown window;
//
//	lifts the un-reset guard so the network can restore what
//	we served offline
//
// senderIsReconnecting — true when the snapshot sender has ReconnectedNode set;
//
//	prevents their stale INACTIVE from overwriting our ACTIVE
//	view of their entry
func mergePeerEntry(
	local, received [][2]RequestState,
	isOwnEntry bool,
	localIsReconnecting bool,
	senderIsReconnecting bool,
) [][2]RequestState {
	merged := make([][2]RequestState, len(received))
	for floor := range received {
		for btn := range received[floor] {
			localState := UNKNOWN
			if local != nil {
				localState = local[floor][btn]
			}

			switch {
			// --- Own-entry protection (isOwnEntry == true) ---

			case isOwnEntry && shouldResetState(localState, received[floor][btn]):
				// A peer is trying to reset our own ACTIVE → INACTIVE.
				// Normally we block this (only we can reset our own entry).
				// Exception: if we are reconnecting we WANT the network to
				// correct us — we may have served this offline and the network
				// still thinks it's pending.  Allow the reset.
				if localIsReconnecting {
					log.Log("[MERGE] own entry floor=%d btn=%d: ACTIVE→INACTIVE allowed (localIsReconnecting)", floor, btn)
					merged[floor][btn] = received[floor][btn]
				} else {
					merged[floor][btn] = localState
				}

			case isOwnEntry && isUnResettingState(localState, received[floor][btn]):
				// A peer is trying to un-reset our own INACTIVE → ACTIVE.
				// Normally we block this (un-resetting is forbidden by the
				// cyclic counter).
				// Exception: if we are reconnecting, the network's ACTIVE
				// represents a request we served while offline — we must adopt
				// it so the manager can re-assign the floor.
				if localIsReconnecting {
					log.Log("[MERGE] own entry floor=%d btn=%d: INACTIVE→ACTIVE allowed (localIsReconnecting)", floor, btn)
					merged[floor][btn] = received[floor][btn]
				} else {
					merged[floor][btn] = localState
				}

			// --- Sender-reconnecting protection (non-own entry) ---

			case !isOwnEntry && senderIsReconnecting && shouldResetState(localState, received[floor][btn]):
				// The sender is in its reconnect window.  Its INACTIVE values
				// are stale offline serves that must not overwrite our ACTIVE
				// view of it.  Keep our own local state.
				log.Log("[MERGE] peer entry floor=%d btn=%d: blocking stale INACTIVE from reconnecting sender", floor, btn)
				merged[floor][btn] = localState

			case isUnResettingState(localState, received[floor][btn]):
				// Standard cyclic-counter un-reset protection for non-own
				// entries: never let a peer move us from INACTIVE back to
				// ACTIVE (that would circumvent the reset protocol).
				merged[floor][btn] = localState

			default:
				merged[floor][btn] = returnValidState(localState, received[floor][btn])
			}
		}
	}
	return merged
}

func mergeHallRequests(
	local, received map[string][][2]RequestState,
	localID string,
	localIsReconnecting bool,
	senderIsReconnecting bool,
) map[string][][2]RequestState {
	merged := copyHallRequests(local)

	for nodeID, receivedEntry := range received {
		isOwnEntry := (nodeID == localID)
		merged[nodeID] = mergePeerEntry(
			local[nodeID],
			receivedEntry,
			isOwnEntry,
			localIsReconnecting,
			senderIsReconnecting,
		)
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
				Behaviour:      receivedState.Behaviour,
				Floor:          receivedState.Floor,
				Direction:      receivedState.Direction,
				DoorOpen:       receivedState.DoorOpen,
				CabRequests:    mergedCabs,
				IsOutOfService: receivedState.IsOutOfService,
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
				Behaviour:      localState.Behaviour,
				Floor:          localState.Floor,
				Direction:      localState.Direction,
				DoorOpen:       recoveredDoorOpen,
				CabRequests:    mergedCabs,
				IsOutOfService: localState.IsOutOfService,
			}
		}
	}
	return merged
}

// FilteredMessage merges a received snapshot into the local one using the
// cyclic counter algorithm.
//
// localIsReconnecting should be set to currentSnapshot.ReconnectedNode at the
// call site so the merge layer knows to lift the un-reset guard on our own
// hall entry (allowing the network to restore requests we served while offline).
func FilteredMessage(local, received NetworkSnapshot, localIsReconnecting bool) NetworkSnapshot {
	log.Log("FilteredMessage: merging snapshot from %s (iter %d) reconnecting=%v",
		received.NodeID, received.Iter, received.ReconnectedNode)
	merged := local
	merged.Elevators = mergeElevators(local, received)
	merged.HallRequests = mergeHallRequests(
		local.HallRequests,
		received.HallRequests,
		local.NodeID,
		localIsReconnecting,
		received.ReconnectedNode,
	)
	log.Log("FilteredMessage: result hallRequests=%v", merged.HallRequests)
	log.Log("FilteredMessage: result elevators=%v", merged.Elevators)
	return merged
}

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
// of the received snapshot reports INACTIVE for that floor/button in its own
// entry.  This propagates served-request resets from the elevator that served them.
//
// If the sender has ReconnectedNode set, its INACTIVEs are stale offline serves
// and must NOT be treated as fresh reset signals — we return early.
func propagateResetsToOwn(snapshot, received NetworkSnapshot) NetworkSnapshot {
	if received.NodeID == snapshot.NodeID {
		return snapshot
	}

	// Guard: sender is in reconnect window — its INACTIVEs are not trustworthy.
	if received.ReconnectedNode {
		log.Log("[propagateResetsToOwn] skipping sender=%s (ReconnectedNode=true)", received.NodeID)
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
		log.Log("allLivePeersKnowRequestedCabs: no consensusPeers, soloMode=%v → %v", soloMode, soloMode)
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerElev, ok := knownStates[peerID].Elevators[snapshot.NodeID]
		if !ok {
			log.Log("allLivePeersKnowRequestedCabs: peer=%s has no entry for nodeID=%s", peerID, snapshot.NodeID)
			return false
		}
		peerStatus := peerElev.CabRequests[floor]
		log.Log("allLivePeersKnowRequestedCabs: peer=%s status=%d (need >= REQUESTED=%d)", peerID, peerStatus, REQUESTED)
		if peerStatus < REQUESTED {
			log.Log("allLivePeersKnowRequestedCabs: peer %s vetoes (status %d)", peerID, peerStatus)
			return false
		}
	}
	log.Log("allLivePeersKnowRequestedCabs: all consensusPeers >= REQUESTED → ACTIVE")
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
		peerEntry, ok := knownStates[peerID].HallRequests[snapshot.NodeID]
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

// AdvanceHallToActive promotes own hall requests from REQUESTED to ACTIVE for
// every floor/button where all live peers have reached at least REQUESTED.
// Requiring full peer consensus before going ACTIVE ensures propagateResetsToOwn
// is never triggered by a peer that is simply unaware of the request.
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
					log.Log("AdvanceHallToActive: floor=%d btn=%d REQUESTED→ACTIVE peers=%v", f, b, livePeerIDs)
					ownRequests[f][b] = ACTIVE
				} else {
					log.Log("AdvanceHallToActive: floor=%d btn=%d REQUESTED but peers not ready peers=%v", f, b, livePeerIDs)
				}
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}
