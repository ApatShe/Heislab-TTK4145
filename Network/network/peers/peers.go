package peers

import (
	log "Heislab/Log"
	"Heislab/Network/network/conn"
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
			log.Log("TX heartbeat: %s → %s:%d", id, broadcastIP, port)
		}
	}
}

func PeersReceiver(port int, peerUpdateCh chan<- PeerUpdate) {
	log.Log("Receiver starting on UDP port %d", port)

	// Same pattern as BcastReceiver — conn.DialBroadcastUDP handles
	// SO_REUSEADDR + broadcast flag, works across multiple processes
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
					log.Log("RX TIMEOUT: lost peer %q (age %v)", id, now.Sub(t))
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
			log.Log("RX state: peers=%v new=%q lost=%v", peersList, p.New, lost)

		default:
			c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, addr, err := c.ReadFrom(buf[0:])
			if err == nil && n > 0 {
				id := string(buf[:n])
				if id != "" && len(id) < 16 {
					oldLen := len(lastSeen)
					lastSeen[id] = time.Now()
					if len(lastSeen) > oldLen {
						log.Log("RX NEW PEER %q from %v (total %d)", id, addr, len(lastSeen))
					}
				}
			}
		}
	}
}
