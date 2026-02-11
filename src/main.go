package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./gotorrent \"magnet_link\"")
	}

	magnet_data, err := ParseMagnetLink(os.Args[1])

	if err != nil {
		log.Fatal(err)
	}
	tracker, err := NewTrackerConnection(magnet_data.trackers[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Connected to tracker")
	err = tracker.Intitate()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Initiated")

	peers, err := tracker.Announce(magnet_data.hashes[0], 10)

	for index, peer := range peers {
		fmt.Printf("%v. SOCKET: %v", index, toSocketAddr(&peer))
		err := InitiatePeerConnection(&peer, [20]string(magnet_data.hashes[0]), tracker.peer_id)
		if err != nil {
			log.Fatal(err)
		}
	}

}
