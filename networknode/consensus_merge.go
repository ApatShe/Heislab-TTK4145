package networknode

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

func returnValidState(local, received RequestState) RequestState {
	if shouldAdvanceState(local, received) {
		return received
	}
	return local
}

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

			case isOwnEntry && shouldResetState(localState, received[floor][btn]):
				if localIsReconnecting {
					merged[floor][btn] = received[floor][btn]
				} else {
					merged[floor][btn] = localState
				}

			case isOwnEntry && isUnResettingState(localState, received[floor][btn]):
				if localIsReconnecting {
					merged[floor][btn] = received[floor][btn]
				} else {
					merged[floor][btn] = localState
				}

			case !isOwnEntry && senderIsReconnecting && shouldResetState(localState, received[floor][btn]):
				merged[floor][btn] = localState

			case isUnResettingState(localState, received[floor][btn]):
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

	for nodeID, receivedState := range received.Elevators {
		localState, exists := local.Elevators[nodeID]

		if receivedState.Floor == -1 {
			continue
		}

		var currentCabs []RequestState
		if exists {
			currentCabs = localState.CabRequests
		}
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
			recoveredDoorOpen := localState.DoorOpen
			if nodeID == local.NodeID && localState.Floor == -1 {
				recoveredDoorOpen = receivedState.DoorOpen
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

func FilteredMessage(local, received NetworkSnapshot, localIsReconnecting bool) NetworkSnapshot {
	merged := local
	merged.Elevators = mergeElevators(local, received)
	merged.HallRequests = mergeHallRequests(
		local.HallRequests,
		received.HallRequests,
		local.NodeID,
		localIsReconnecting,
		received.ReconnectedNode,
	)
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
					ownRequests[floor][btn] = REQUESTED
					break
				}
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}

func propagateResetsToOwn(snapshot, received NetworkSnapshot) NetworkSnapshot {
	if received.NodeID == snapshot.NodeID {
		return snapshot
	}

	if received.ReconnectedNode {
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

	if len(consensusPeers) == 0 {
		soloMode := len(livePeerIDs) <= 1
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerElev, ok := knownStates[peerID].Elevators[snapshot.NodeID]
		if !ok {
			return false
		}
		peerStatus := peerElev.CabRequests[floor]
		if peerStatus < REQUESTED {
			return false
		}
	}
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

	if len(consensusPeers) == 0 {
		soloMode := len(livePeerIDs) <= 1
		return soloMode
	}

	for _, peerID := range consensusPeers {
		peerEntry, ok := knownStates[peerID].HallRequests[snapshot.NodeID]
		if !ok {
			return false
		}
		peerStatus := peerEntry[floor][button]
		if peerStatus < REQUESTED {
			return false
		}
	}

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
				elev.CabRequests[floor] = ACTIVE
			} else {
			}
		}
	}
	snapshot.Elevators[snapshot.NodeID] = elev
	return snapshot
}

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
					ownRequests[f][b] = ACTIVE
				} else {
				}
			}
		}
	}
	snapshot.HallRequests[snapshot.NodeID] = ownRequests
	return snapshot
}
