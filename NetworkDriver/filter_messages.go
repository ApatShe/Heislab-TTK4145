package networkdriver

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
	if isUnResettingState(local, received) {
		return false
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
	merged := make([]RequestState, len(local))
	copy(merged, local)

	if localID != receivedID {
		return merged // never accept another node's cab requests
	}

	for floor := range local {
		if local[floor] == UNKNOWN {
			merged[floor] = received[floor] // recover state on restart
		}
	}
	return merged
}

// mergeHallRequests merges a received hall request matrix into the local one.
// Hall requests are shared across all nodes on the network.
// copyHallRequests shallow-copies a per-peer hall request map.
func copyHallRequests(src map[string][][2]RequestState) map[string][][2]RequestState {
	dst := make(map[string][][2]RequestState, len(src))
	for nodeID, reqs := range src {
		dst[nodeID] = reqs
	}
	return dst
}

// mergePeerEntry applies the cyclic counter to a single peer's floor/button matrix.
func mergePeerEntry(local, received [][2]RequestState) [][2]RequestState {
	merged := make([][2]RequestState, len(received))
	for floor := range received {
		for btn := range received[floor] {
			var localState RequestState
			if local != nil {
				localState = local[floor][btn]
			}
			merged[floor][btn] = returnValidState(localState, received[floor][btn])
		}
	}
	return merged
}

// mergeHallRequests merges only the received node's own entry into the local map.
func mergeHallRequests(local, received map[string][][2]RequestState, receivedID string) map[string][][2]RequestState {
	merged := copyHallRequests(local)
	merged[receivedID] = mergePeerEntry(local[receivedID], received[receivedID])
	return merged
}

func copyElevators(src map[string]ElevatorState) map[string]ElevatorState {
	dst := make(map[string]ElevatorState, len(src))
	for nodeID, state := range src {
		dst[nodeID] = state
	}
	return dst
}

func mergePeerElevator(local, received NetworkSnapshot) ElevatorState {
	receivedNodeState := received.Elevators[received.NodeID]
	return ElevatorState{
		Behaviour: receivedNodeState.Behaviour,
		Floor:     receivedNodeState.Floor,
		Direction: receivedNodeState.Direction,
		CabRequests: mergeCabRequests(
			local.Elevators[received.NodeID].CabRequests, // our stored copy of this peer's cabs
			receivedNodeState.CabRequests,
			received.NodeID,
			received.NodeID,
		),
	}
}

func mergeElevators(local, received NetworkSnapshot) map[string]ElevatorState {
	merged := copyElevators(local.Elevators)
	merged[received.NodeID] = mergePeerElevator(local, received)
	return merged
}

func FilteredMessage(local, received NetworkSnapshot) NetworkSnapshot {
	merged := local
	merged.Elevators = mergeElevators(local, received)
	merged.HallRequests = mergeHallRequests(local.HallRequests, received.HallRequests, received.NodeID)
	return merged
}

// shouldAdoptHallRequest returns true when own entry for floorIndex/buttonIndex
// is INACTIVE and at least one peer has already reached REQUESTED or higher.
// This is the adoption predicate: case B of the INACTIVE→REQUESTED transition.
func shouldAdoptHallRequest(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
	ownRequest := snapshot.HallRequests[snapshot.NodeID][floorIndex][buttonIndex]
	return ownRequest == INACTIVE && isHallRequestKnownByAnyPeer(snapshot, floorIndex, buttonIndex)
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
				ownRequests[floorIndex][buttonIndex] = REQUESTED
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}

// isHallRequestKnownByAnyPeer returns true when at least one peer (excluding
// self) has reached REQUESTED or higher for floorIndex/buttonIndex.
func isHallRequestKnownByAnyPeer(snapshot NetworkSnapshot, floorIndex int, buttonIndex int) bool {
	for peerID, peerRequests := range snapshot.HallRequests {
		if peerID == snapshot.NodeID {
			continue
		}
		if peerRequests != nil && peerRequests[floorIndex][buttonIndex] >= REQUESTED {
			return true
		}
	}
	return false
}

// allPeersHaveRequested returns true when every peer in activePeerIDs has
// acknowledged floorIndex/buttonIndex with at least a REQUESTED state.
// This is the consensus predicate for the cyclic counter promotion step.
func allPeersHaveRequested(snapshot NetworkSnapshot, activePeerIDs []string, floorIndex int, buttonIndex int) bool {
	for _, peerID := range activePeerIDs {
		peerRequests := snapshot.HallRequests[peerID]
		if peerRequests == nil || peerRequests[floorIndex][buttonIndex] < REQUESTED {
			return false
		}
	}
	return true
}

// withUnknownsFlippedToInactive returns a copy of reqs where every UNKNOWN
// entry has been replaced with INACTIVE. Used when a node first appears on
// the peer list and can safely assume it has no pending hall requests.
func withUnknownsFlippedToInactive(reqs [][2]RequestState) [][2]RequestState {
	result := make([][2]RequestState, len(reqs))
	copy(result, reqs)
	for floorIndex := range result {
		for buttonIndex := range result[floorIndex] {
			if result[floorIndex][buttonIndex] == UNKNOWN {
				result[floorIndex][buttonIndex] = INACTIVE
			}
		}
	}
	return result
}

// AdvanceToActive promotes hall requests from REQUESTED to ACTIVE for every
// floor/button where all active peers have reached at least REQUESTED.
// This is the consensus promotion step: a confirmed request the HRA will act on.
func AdvanceToActive(snapshot NetworkSnapshot, activePeerIDs []string) NetworkSnapshot {
	ownRequests := snapshot.HallRequests[snapshot.NodeID]
	if ownRequests == nil {
		return snapshot
	}
	for floorIndex := 0; floorIndex < len(ownRequests); floorIndex++ {
		for buttonIndex := 0; buttonIndex < 2; buttonIndex++ {
			isAlreadyActive := ownRequests[floorIndex][buttonIndex] == ACTIVE
			if isAlreadyActive {
				continue
			}
			if allPeersHaveRequested(snapshot, activePeerIDs, floorIndex, buttonIndex) {
				ownRequests[floorIndex][buttonIndex] = ACTIVE
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}
