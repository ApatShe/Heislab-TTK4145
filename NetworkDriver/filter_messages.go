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
func mergeHallRequests(local, received [][2]RequestState) [][2]RequestState {
	mergedHallRequests := make([][2]RequestState, len(local))
	for floor := range local {
		for btn := range local[floor] {
			mergedHallRequests[floor][btn] = returnValidState(local[floor][btn], received[floor][btn])
		}
	}
	return mergedHallRequests
}

func FilteredMessage(local, received NetworkSnapshot) NetworkSnapshot {
	merged := local
	merged.CabRequests = mergeCabRequests(local.CabRequests, received.CabRequests, local.NodeID, received.NodeID)
	merged.HallRequests = mergeHallRequests(local.HallRequests, received.HallRequests)
	return merged
}
