package networkdriver

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
func mergeCabRequests(local, received []RequestState, localID, receivedID string) []RequestState {
	size := len(received)
	if len(local) > size {
		size = len(local)
	}
	merged := make([]RequestState, size)
	copy(merged, local)

	if localID != receivedID {
		return merged
	}

	for floor := range local {
		if local[floor] == UNKNOWN {
			merged[floor] = received[floor]
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

// mergePeerEntry applies the cyclic counter to a single peer's floor/button matrix.
func mergePeerEntry(local, received [][2]RequestState, isOwnEntry bool) [][2]RequestState {
	merged := make([][2]RequestState, len(received))
	for floor := range received {
		for btn := range received[floor] {
			localState := UNKNOWN
			if local != nil {
				localState = local[floor][btn]
			}

			if isOwnEntry {
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

	// Iterate through EVERY elevator entry in the received snapshot.
	// This allows a restarted node to find its own previous ID (and Cabs)
	// inside a peer's snapshot.
	for nodeID, receivedState := range received.Elevators {
		localState, exists := local.Elevators[nodeID]

		var currentCabs []RequestState
		if exists {
			currentCabs = localState.CabRequests
		}

		// Merge the states. If it's our own ID, mergeCabRequests will
		// transition UNKNOWN -> ACTIVE/INACTIVE based on the peer's data.
		merged[nodeID] = ElevatorState{
			Behaviour: receivedState.Behaviour,
			Floor:     receivedState.Floor,
			Direction: receivedState.Direction,
			DoorOpen:  receivedState.DoorOpen,
			CabRequests: mergeCabRequests(
				currentCabs,
				receivedState.CabRequests,
				nodeID, // The ID of the elevator being merged
				nodeID, // The ID of the data source
			),
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
	return merged
}

// shouldAdoptHallRequest returns true when own entry for floorIndex/buttonIndex
// is INACTIVE and at least one peer has already reached REQUESTED.
// This is the adoption predicate: case B of the INACTIVE→REQUESTED transition.
func shouldAdoptHallRequest(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
	ownRequest := snapshot.HallRequests[snapshot.NodeID][floorIndex][buttonIndex]
	return (ownRequest == UNKNOWN || ownRequest == INACTIVE) && isHallRequestKnownByAnyPeer(snapshot, floorIndex, buttonIndex)
}

// adoptHallRequestsFromPeers advances own hall-request entries to REQUESTED
// for every floor/button where shouldAdoptHallRequest is true.
func adoptHallRequestsFromPeers(snapshot NetworkSnapshot) NetworkSnapshot {
	ownRequests := snapshot.HallRequests[snapshot.NodeID]
	if ownRequests == nil {
		return snapshot
	}
	for floorIndex := range ownRequests {
		for buttonIndex := range ownRequests[floorIndex] {
			if shouldAdoptHallRequest(snapshot, floorIndex, buttonIndex) {
				log.Log("adoptHallRequestsFromPeers: floor=%d btn=%d adopted→REQUESTED", floorIndex, buttonIndex)
				ownRequests[floorIndex][buttonIndex] = REQUESTED
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}

// propagateResetsToOwn clears own ACTIVE entries to INACTIVE when the sender
// of the received snapshot reports INACTIVE for that floor/button in its own entry.
// This propagates served-request resets from the elevator that served them.
func propagateResetsToOwn(snapshot NetworkSnapshot, received NetworkSnapshot) NetworkSnapshot {
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
	for f := range own {
		for b := 0; b < 2; b++ {
			if own[f][b] == ACTIVE && senderEntry[f][b] == INACTIVE {
				log.Log("propagateResetsToOwn: floor=%d btn=%d ACTIVE→INACTIVE (reset by %s)", f, b, received.NodeID)
				own[f][b] = INACTIVE
				changed = true
			}
		}
	}
	if changed {
		snapshot.HallRequests[snapshot.NodeID] = own
	}
	return snapshot
}

// isHallRequestKnownByAnyPeer returns true when at least one peer (excluding
// self) has reached exactly REQUESTED for floorIndex/buttonIndex.
// Checking == REQUESTED (not >= REQUESTED) prevents re-adoption after serving:
// a peer still at ACTIVE after the local node served should not trigger a re-raise.
func isHallRequestKnownByAnyPeer(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
	for peerID, peerRequests := range snapshot.HallRequests {
		if peerID == snapshot.NodeID {
			continue
		}
		if peerRequests != nil && peerRequests[floorIndex][buttonIndex] == REQUESTED {
			return true
		}
	}
	return false
}

func allLivePeersKnowRequest(
	snapshot NetworkSnapshot,
	livePeerIDs []string,
	knownStates map[string]NetworkSnapshot,
	f, b int,
) bool {
	// consensusPeers = livePeerIDs ∩ knownStates (excluding self)
	// livePeerIDs: currently online peers
	// knownStates: persistent snapshots — includes dead peers, but they
	//              won't appear in livePeerIDs so they never block consensus
	consensusPeers := make([]string, 0)
	for _, peerID := range livePeerIDs {
		if peerID == snapshot.NodeID {
			continue
		}
		if _, known := knownStates[peerID]; known {
			consensusPeers = append(consensusPeers, peerID)
		}
	}

	log.Log("allLivePeersKnowRequest(floor=%d btn=%d): livePeerIDs=%v consensusPeers=%v ownStatus=%d",
		f, b, livePeerIDs, consensusPeers, snapshot.HallRequests[snapshot.NodeID][f][b])

	// No intersection — either truly solo or peers not yet heard from.
	// Only allow advance in genuine solo mode (self is the only live peer).
	if len(consensusPeers) == 0 {
		soloMode := len(livePeerIDs) <= 1
		log.Log("allLivePeersKnowRequest: no consensusPeers, soloMode=%v → %v", soloMode, soloMode)
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerStatus := snapshot.HallRequests[peerID][f][b]
		log.Log("allLivePeersKnowRequest: peer=%s status=%d (need >= REQUESTED=%d)", peerID, peerStatus, REQUESTED)
		if peerStatus < REQUESTED {
			log.Log("allLivePeersKnowRequest: peer %s vetoes (status %d)", peerID, peerStatus)
			return false
		}
	}

	log.Log("allLivePeersKnowRequest: all consensusPeers >= REQUESTED → ACTIVE")
	return true
}

// AdvanceToActive promotes own hall requests from REQUESTED to ACTIVE for every
// floor/button where all live peers have reached at least REQUESTED.
// Requiring full peer consensus before going ACTIVE ensures propagateResetsToOwn
// is never triggered by a peer that is simply unaware of the request.
// filter_message.go
func AdvanceToActive(
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
				if allLivePeersKnowRequest(snapshot, livePeerIDs, knownStates, f, b) {
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
