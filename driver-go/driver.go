package driver

/*
func PollObstructionAndSend(out chan<- bool) {
	/*
		Code to run with channel from door.go:
		doorCh := door.RunDoor()
		go driver.PollObstructionAndSend(doorCh.DoorObstructionChan)
	*
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


*/
