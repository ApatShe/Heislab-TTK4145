package networkdriver

import (
	elevatorcontroller "Heislab/ElevatorController"
	"Heislab/Network/network/bcast"
	"Heislab/Network/network/peers"
	"Heislab/driver-go/elevio"
	"fmt"
)

// Runs a networking node. Distributes & acknowledges messages while maintaining a list
// of peers on the network
func RunNetworkNode(
	buttonEventChan <-chan elevio.ButtonEvent,
	elevatorStateChan <-chan elevatorcontroller.Elevator,
	orderChan chan<- elevio.ButtonEvent,
	peerStates chan<- []elevatorcontroller.Elevator,

	initState elevatorcontroller.Elevator,
	id string,
) {
	//nodeInstance := newNode(initState)
	nodeID = id
	uptime = 0

	// Internal channels — not to be confused with the peer update channels below
	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)
	stateTx := make(chan NetworkSnapshot)
	stateRx := make(chan NetworkSnapshot)

	go peers.Transmitter(port, id, peerTxEnable)
	go peers.Receiver(port, peerUpdateCh)
	go bcast.Transmitter(stateBroadcastPort, stateTx)
	go bcast.Receiver(stateBroadcastPort, stateRx)

	for {
		select {

		case peer := <-peerUpdateCh:
			fmt.Printf("Peer update:\n")
			fmt.Printf("  Peers: %q\n", peer.Peers)
			fmt.Printf("  New:   %q\n", peer.New)
			fmt.Printf("  Lost:  %q\n", peer.Lost)
			for _, lost := range peer.Lost {
				delete(knownStates, lost)
			}

		case state := <-stateRx:
			fmt.Printf("Received snapshot:\n")
			fmt.Printf("  ID:           %s\n", state.NodeID)
			fmt.Printf("  Behaviour:    %s\n", state.Elevators[state.NodeID].Behaviour)
			fmt.Printf("  Floor:        %d\n", state.Elevators[state.NodeID].Floor)
			fmt.Printf("  Direction:    %s\n", state.Elevators[state.NodeID].Direction)
			fmt.Printf("  CabRequests:  %v\n", state.Elevators[state.NodeID].CabRequests)
			fmt.Printf("  HallRequests: %v\n", state.HallRequests)

			fmt.Printf("Received state from %s (iter %d)\n", state.NodeID, state.Iter)
			fmt.Printf("Known states: %d peers\n", len(knownStates))

		}
	}
}
