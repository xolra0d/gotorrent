package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./gotorrent magnet_link")
	}

	magnet_data, err := ParseMagnetLink(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Magnet data:", magnet_data)

	tracker, err := NewTrackerConnection("open.demonii.com:1337")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Connected")

	err = tracker.Intitate()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Initiated")

	peers, err := tracker.Announce("CE7874BCBDBFEFBF55D494EFEE629583C2884E26", 10)

	for index, peer := range peers {
		fmt.Printf("%v. SOCKET: %v", index, toSocketAddr(&peer))
	}
}
