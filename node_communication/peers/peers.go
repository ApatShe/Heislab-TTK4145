package peers

import (
	"Heislab/node_communication/conn"
	"fmt"
	"net"
	"sort"
	"time"
)

type PeerUpdate struct {
	Peers []string
	New   string
	Lost  []string
}

const interval = 15 * time.Millisecond
const timeout = 500 * time.Millisecond

// Mirrors bcast.broadcastIP — set once from main via SetBroadcastAddr
var broadcastIP = "255.255.255.255"

func SetBroadcastAddr(addr string) {
	broadcastIP = addr
}

func PeersTransmitter(port int, id string, transmitEnable <-chan bool) {
	c := conn.DialBroadcastUDP(port)
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", broadcastIP, port))
	enable := true
	for {
		select {
		case enable = <-transmitEnable:
		case <-time.After(interval):
		}
		if enable {
			c.WriteTo([]byte(id), addr)
		}
	}
}

func PeersReceiver(port int, peerUpdateCh chan<- PeerUpdate) {
	c := conn.DialBroadcastUDP(port)

	var buf [1024]byte
	lastSeen := make(map[string]time.Time)
	prevPeers := make(map[string]bool)

	ticker := time.NewTicker(600 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			lost := []string{}
			for id, t := range lastSeen {
				if now.Sub(t) > timeout {
					lost = append(lost, id)
					delete(lastSeen, id)
				}
			}

			peersList := []string{}
			for id := range lastSeen {
				peersList = append(peersList, id)
			}
			sort.Strings(peersList)
			sort.Strings(lost)

			p := PeerUpdate{
				Peers: peersList,
				Lost:  lost,
			}

			for _, pid := range peersList {
				if !prevPeers[pid] {
					p.New = pid
					break
				}
			}

			prevPeers = make(map[string]bool, len(peersList))
			for _, pid := range peersList {
				prevPeers[pid] = true
			}

			peerUpdateCh <- p

		default:
			c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, _, err := c.ReadFrom(buf[0:])
			if err == nil && n > 0 {
				id := string(buf[:n])
				if id != "" && len(id) < 16 {
					lastSeen[id] = time.Now()
				}
			}
		}
	}
}
