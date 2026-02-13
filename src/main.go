package main

import (
	"context"
	"encoding/hex"
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

	d, _ := hex.DecodeString(magnet_data.hashes[0])
	fmt.Println(d)
	log.Fatal("")
	var tracker_wg sync.WaitGroup
	peer_id := RandomPeerId()
	peers := make(chan netip.AddrPort, 1000)
	trackers := make(chan TrackerConnection, 10)
	for _, tracker_ip := range magnet_data.trackers {
		tracker_wg.Go(func() { GetPeers(context.Background(), tracker_ip, peer_id, magnet_data.hashes[0], peers, trackers) })
	}

	go func() {
		tracker_wg.Wait()
		close(peers)
	}()

	var peer_wg sync.WaitGroup
	for peer := range peers {
		fmt.Println(peer)
		// peer_wg.Go(func() { InitiatePeerConnection(context.Background(), peer, []byte(magnet_data.hashes[0]), peer_id) })
	}

	tracker_wg.Wait()
	peer_wg.Wait()
}
