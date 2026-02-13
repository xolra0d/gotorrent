package main

import (
	"fmt"
	"net"
	"net/netip"
)

type PeerStatus int

type PeerInfo struct {
	ip   netip.Addr
	port uint16
}

func InitiatePeerConnection(peer_info netip.AddrPort, hash []byte, my_peer_id [20]byte) error {
	reserved := [8]byte{}

	handshake_bytes := make([]byte, 0, 68)
	handshake_bytes = append(handshake_bytes, 19)
	handshake_bytes = append(handshake_bytes, []byte("BitTorrent protocol")...)
	handshake_bytes = append(handshake_bytes, reserved[:]...)
	handshake_bytes = append(handshake_bytes, hash[:]...)
	handshake_bytes = append(handshake_bytes, my_peer_id[:]...)

	conn, err := net.Dial("tcp", peer_info.String())
	if err != nil {
		return err
	}
	n, err := conn.Write(handshake_bytes)
	if err != nil {
		return err
	} else if n != 68 {
		return fmt.Errorf("Expected to send 68 bytes, sent %v instead", n)
	}
	fmt.Println("written successfully")
	n, err = conn.Read(handshake_bytes)
	fmt.Println("written successfully")
	if err != nil {
		return err
	} else if n != 68 {
		return fmt.Errorf("Expected to send 68 bytes, sent %v instead", n)
	}

	fmt.Printf("%v\n", handshake_bytes)

	return nil
}
