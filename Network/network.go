package network

/*
import (
	"Heislab/Network/network/bcast"
	"Heislab/Network/network/localip"
	"Heislab/Network/network/peers"
	networkdriver "Heislab/NetworkDriver"
	"flag"
	"fmt"
	"os"
	"time"
)

type ElevatorSnapshotMsg struct {
	ID           string
	Behaviour    string
	Floor        int
	Direction    string
	CabRequests  []networkdriver.RequestState
	HallRequests [][2]networkdriver.RequestState
	Iter         int
}

func NetworkExample() {
	var id string
	flag.StringVar(&id, "id", "", "id of this peer")
	flag.Parse()

	if id == "" {
		localIP, err := localip.LocalIP()
		if err != nil {
			fmt.Println(err)
			localIP = "DISCONNECTED"
		}
		id = fmt.Sprintf("peer-%s-%d", localIP, os.Getpid())
	}

	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)
	go peers.Transmitter(15647, id, peerTxEnable)
	go peers.Receiver(15647, peerUpdateCh)

	stateTx := make(chan ElevatorSnapshotMsg)
	stateRx := make(chan ElevatorSnapshotMsg)
	go bcast.Transmitter(16569, stateTx)
	go bcast.Receiver(16569, stateRx)

	go func() {
			msg := FilteredMessage()
		for {
			msg.Iter++
			stateTx <- msg
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Accumulate peer states for cost function
	knownStates := make(map[string]networkdriver.NetworkSnapshot)

	fmt.Println("Started")
	for {
		select {
		case p := <-peerUpdateCh:
			fmt.Printf("Peer update:\n")
			fmt.Printf("  Peers: %q\n", p.Peers)
			fmt.Printf("  New:   %q\n", p.New)
			fmt.Printf("  Lost:  %q\n", p.Lost)
			for _, lost := range p.Lost {
				delete(knownStates, lost)
			}

		case a := <-stateRx:
			fmt.Printf("Received snapshot:\n")
			fmt.Printf("  ID:           %s\n", a.ID)
			fmt.Printf("  Behaviour:    %s\n", a.Behaviour)
			fmt.Printf("  Floor:        %d\n", a.Floor)
			fmt.Printf("  Direction:    %s\n", a.Direction)
			fmt.Printf("  CabRequests:  %v\n", a.CabRequests)
			fmt.Printf("  HallRequests: %v\n", a.HallRequests)

			fmt.Printf("Received state from %s (iter %d)\n", a.ID, a.Iter)
			fmt.Printf("Known states: %d peers\n", len(knownStates))

		}
	}
}
*/
