package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
)

// const BitTorrentProtocolStr = []byte("BitTorrent protocol")
const BitTorrentProtocolStr = "12121"

func generateHandshakeBytes(hash []byte, my_peer_id [20]byte) []byte {
	reserved := [8]byte{}

	handshake_bytes := make([]byte, 0, 68)
	handshake_bytes = append(handshake_bytes, byte(len(BitTorrentProtocolStr)))
	handshake_bytes = append(handshake_bytes, BitTorrentProtocolStr...)
	handshake_bytes = append(handshake_bytes, reserved[:]...)
	handshake_bytes = append(handshake_bytes, hash[:]...)
	handshake_bytes = append(handshake_bytes, my_peer_id[:]...)

	return handshake_bytes
}

func decodeHandshakeBytes(buffer []byte) error {
	if len(buffer) != 68 {
		return nil
	} else if buffer[0] != byte(len(BitTorrentProtocolStr)) {
		return nil
	} else if !slices.Equal(buffer[1:20], []byte(BitTorrentProtocolStr)) {
		return nil
	}

	return nil
}

func initiatePeerConnection(ctx context.Context, peer_info netip.AddrPort, hash []byte, my_peer_id [20]byte) error {
	handshake_bytes := generateHandshakeBytes(hash, my_peer_id)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", peer_info.String())
	if err != nil {
		return err
	}
	n, err := conn.Write(handshake_bytes)
	if err != nil {
		return err
	}
	// else if n != 68 {
	// 	return fmt.Errorf("Expected to send 68 bytes, sent %v instead", n)
	// }
	fmt.Println("written successfully", n)
	n, err = conn.Read(handshake_bytes)
	if err != nil {
		return err
	}
	fmt.Println("read successfully,", n)
	// else if n != 68 {
	// 	return fmt.Errorf("Expected to send 68 bytes, sent %v instead", n)
	// }

	fmt.Printf("%v\n", handshake_bytes)

	return nil
}
