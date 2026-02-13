package main

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"os"
	"sync"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./gotorrent \"magnet_link\"")
	}

	magnet_data, err := ParseMagnetLink(os.Args[1])
	if err != nil {
		log.Fatal(err)
	} else if len(magnet_data.trackers) == 0 {
		log.Fatal("No trackers specified")
	} else if len(magnet_data.hashes) == 0 {
		log.Fatal("No hashes specified")
	} else if magnet_data.name == "" {
		magnet_data.name = "torrent"
	}

	peer_id := RandomPeerId()
	var wg sync.WaitGroup

	peers := make(chan netip.AddrPort)
	trackers := make(chan TrackerConnection)
	for _, tracker_ip := range magnet_data.trackers {
		wg.Go(func() { GetPeers(context.Background(), tracker_ip, peer_id, magnet_data.hashes[0], peers, trackers) })
	}

	go func() {
		wg.Wait()
		close(peers)

	}()

	for peer := range peers {
		err := InitiatePeerConnection(peer, []byte(magnet_data.hashes[0]), peer_id)
		if err != nil {
			fmt.Println(err)
		}
	}
}
