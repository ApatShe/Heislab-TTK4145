package manager

import (
	"Heislab/ElevatorController"
	"Heislab/driver-go/elevio"
)

func RunManager(
	snapshotChan <-chan NetworkSnapshot,
	elevStateChan <-chan ElevatorController.Elevator,
	consensusChan chan<- NetworkSnapshot,
	orderChan chan<- elevio.ButtonEvent,
	localID string,
) {
	var localElevatorState ElevatorController.Elevator

	for {
		select {
		case state := <-elevStateChan:
			localElevatorState = state

		case snapshot := <-snapshotChan:
			if !livePeersConsensus(snapshot) {
				continue
			}

			hraInput := convertSnapshotToHRAInput(snapshot, localElevatorState)
			delegatedHallRequests := runHRA(hraInput)

			consensusChan <- snapshot

			for _, designatedHallRequest := range assignedOrdersToButtonEvents(delegatedHallRequests, localID) {
				orderChan <- designatedHallRequest
			}
		}
	}
}
