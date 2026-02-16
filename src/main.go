package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/netip"
	"os"
	"sync"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./gotorrent \"magnet_link\"")
	}

	magnetData, err := ParseMagnetLink(os.Args[1])
	if err != nil {
		log.Fatal(err)
	} else if len(magnetData.trackers) == 0 {
		log.Fatal("No trackers specified")
	} else if len(magnetData.hashes) == 0 {
		log.Fatal("No hashes specified")
	} else if magnetData.name == "" {
		magnetData.name = "torrent"
	}

	hash, err := hex.DecodeString(magnetData.hashes[0])
	if err != nil {
		panic(err)
	}

	var trackerWg sync.WaitGroup
	peerId := RandomPeerId()
	peers := make(chan netip.AddrPort, 1000)
	trackers := make(chan TrackerConnection, 10)
	for _, trackerIp := range magnetData.trackers {
		trackerWg.Go(func() { _ = GetPeers(context.Background(), trackerIp, peerId, magnetData.hashes[0], peers, trackers) })
	}

	go func() {
		trackerWg.Wait()
		close(peers)
	}()

	var peerWg sync.WaitGroup
	for peer := range peers {
		peerWg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*50)
			defer cancel()
			conn, err := initiatePeerConnection(ctx, peer, hash, peerId, nil)
			if err != nil {
				fmt.Printf("PEER %v: ERROR: %v\n", peer, err)
				return
			}
			err = conn.GetMetadata()
			if err != nil {
				fmt.Printf("PEER %v: ERROR: %v\n", peer, err)
				return
			}
			piece, err := conn.DownloadPiece(0)
			if err != nil {
				fmt.Printf("PEER %v: ERROR: %v\n", peer, err)
				return
			}
			fmt.Printf("PEER %v: SUCC: %v\n", peer, sha1.Sum(piece))
		})
	}

	trackerWg.Wait()
	peerWg.Wait()
}
