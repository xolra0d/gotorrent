package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
)

const BitTorrentProtocolStr = "BitTorrent protocol"
const PeerHandshakeLength = 68

type MessageType int8

const (
	Choke MessageType = iota
	Unchoke
	Interested
	NotInterested
	Have
	BitField
	Request
	Piece
	Cancel
	Extension MessageType = 20
)

type PeerMessage struct {
	msgType MessageType
	msgData []byte
}

type PeerConnection struct {
	conn     net.Conn
	backlog  []PeerMessage
	peerId   []byte
	unchoked bool
}

func generateHandshakeBytes(hash []byte, myPeerId [20]byte, needInfo bool) []byte {
	reserved := [8]byte{}

	if needInfo {
		reserved[5] = 0x10 // enable metadata ext
	}

	handshakeBytes := make([]byte, 0, 68)
	handshakeBytes = append(handshakeBytes, byte(len(BitTorrentProtocolStr)))
	handshakeBytes = append(handshakeBytes, BitTorrentProtocolStr...)
	handshakeBytes = append(handshakeBytes, reserved[:]...)
	handshakeBytes = append(handshakeBytes, hash[:]...)
	handshakeBytes = append(handshakeBytes, myPeerId[:]...)

	return handshakeBytes
}

func decodeHandshakeBytes(buffer []byte, hash []byte, needInfo bool) ([]byte, error) {
	if len(buffer) != PeerHandshakeLength {
		return []byte{}, fmt.Errorf("expected to receive %v bytes, got %v instead", PeerHandshakeLength, len(buffer))
	} else if buffer[0] != byte(len(BitTorrentProtocolStr)) {
		return []byte{}, fmt.Errorf("handshake message should start from 19")
	} else if !slices.Equal(buffer[1:20], []byte(BitTorrentProtocolStr)) {
		return []byte{}, fmt.Errorf("wrong handshake bytes. Expected %v, received %v", []byte(BitTorrentProtocolStr), buffer[1:20])
	} else if needInfo && (buffer[24]&0x10) == 0 {
		return []byte{}, fmt.Errorf("peer does not support extended protocol")
	} else if !slices.Equal(buffer[28:48], hash) {
		return []byte{}, fmt.Errorf("wrong handshake bytes. Expected %v, received %v", hash, buffer[20:40])
	}
	peerId := buffer[48:]
	return peerId, nil
}

func initiatePeerConnection(ctx context.Context, peerInfo netip.AddrPort, hash []byte, myPeerId [20]byte, info map[string]any) (PeerConnection, error) {
	handshakeBytes := generateHandshakeBytes(hash, myPeerId, info == nil)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", peerInfo.String())
	if err != nil {
		return PeerConnection{}, err
	}
	n, err := conn.Write(handshakeBytes)
	if err != nil {
		return PeerConnection{}, err
	} else if n != PeerHandshakeLength {
		return PeerConnection{}, fmt.Errorf("expected to send %v bytes, sent %v instead", PeerHandshakeLength, n)
	}
	clear(handshakeBytes)
	n, err = conn.Read(handshakeBytes)
	if err != nil {
		return PeerConnection{}, err
	}
	peerId, err := decodeHandshakeBytes(handshakeBytes, hash, info == nil)
	if err != nil {
		return PeerConnection{}, err
	}
	peerConn := PeerConnection{conn: conn, peerId: peerId}
	return peerConn, nil
}
