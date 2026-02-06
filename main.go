package main

import (
	network "Heislab/Network-go"
	driver "Heislab/driver-go"
	hraexample "Heislab/utilities"
)

func main() {
	go driver.Driver()
	go network.NetworkExample()
	go hraexample.HRAExample()
	select {}
}
