package driver

import (
	"Heislab/driver-go/elevio"
	"fmt"
	"time"
)

func PollObstructionAndSend(out chan<- bool) {
	/*
		Code to run with channel from door.go:
		doorCh := door.RunDoor()
		go driver.PollObstructionAndSend(doorCh.DoorObstructionChan)
	*/
	rawCh := make(chan bool, 10)
	go elevio.PollObstructionSwitch(rawCh)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	latest := false
	haveLatest := false
	prevSent := false
	havePrevSent := false

	for {
		select {
		case v := <-rawCh:
			latest = v
			haveLatest = true
		case <-ticker.C:
			if !haveLatest {
				continue
			}
			if !havePrevSent || latest != prevSent {
				out <- latest
				prevSent = latest
				havePrevSent = true
			}
		}
	}
}

func Driver() {

	numFloors := 4

	elevio.Init("localhost:15657", numFloors)

	drv_buttons := make(chan elevio.ButtonEvent)
	drv_floors := make(chan int)
	drv_obstr := make(chan bool)
	drv_stop := make(chan bool)

	go elevio.PollButtons(drv_buttons)
	go elevio.PollFloorSensor(drv_floors)
	go elevio.PollObstructionSwitch(drv_obstr)
	go elevio.PollStopButton(drv_stop)

	for {
		select {
		case a := <-drv_buttons:
			fmt.Printf("%+v\n", a)
			elevio.SetButtonLamp(a.Button, a.Floor, true)

		case a := <-drv_floors:
			fmt.Printf("Floor: %+v\n", a)
			// Floor sensor updates are just logged, movement is controlled by ElevatorController

		case a := <-drv_obstr:
			fmt.Printf("Obstruction: %+v\n", a)

		case a := <-drv_stop:
			fmt.Printf("Stop button: %+v\n", a)
			for f := 0; f < numFloors; f++ {
				for b := elevio.ButtonType(0); b < 3; b++ {
					elevio.SetButtonLamp(b, f, false)
				}
			}
		}
	}
}
