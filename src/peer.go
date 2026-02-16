package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"

	"github.com/xolra0d/gotorrent/src/benserde"
)

const BitTorrentProtocolStr = "BitTorrent protocol"
const PeerHandshakeLength = 68

type MessageType byte

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
	conn                net.Conn
	stream              bufio.ReadWriter
	backlog             []PeerMessage
	peerId              []byte
	TorrentInfo         map[string]benserde.BencodeValue
	MetadataExtensionId int
	unchoked            bool
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
	} else if needInfo && (buffer[25]&0x10) == 0 {
		return []byte{}, fmt.Errorf("peer does not support extended protocol")
	} else if !slices.Equal(buffer[28:48], hash) {
		return []byte{}, fmt.Errorf("wrong handshake bytes. Expected %v, received %v", hash, buffer[20:40])
	}
	peerId := buffer[48:]
	return peerId, nil
}

func initiatePeerConnection(
	ctx context.Context,
	peerInfo netip.AddrPort,
	hash []byte,
	myPeerId [20]byte,
	info map[string]benserde.BencodeValue,
) (PeerConnection, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", peerInfo.String())
	if err != nil {
		return PeerConnection{}, err
	}
	stream := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	handshakeBytes := generateHandshakeBytes(hash, myPeerId, info == nil)
	n, err := stream.Write(handshakeBytes)
	if err != nil {
		return PeerConnection{}, err
	} else if n != PeerHandshakeLength {
		return PeerConnection{}, fmt.Errorf("expected to send %v bytes, sent %v instead", PeerHandshakeLength, n)
	}
	err = stream.Flush()
	if err != nil {
		return PeerConnection{}, err
	}
	clear(handshakeBytes)
	n, err = stream.Read(handshakeBytes)
	if err != nil {
		return PeerConnection{}, err
	}
	peerId, err := decodeHandshakeBytes(handshakeBytes, hash, info == nil)
	if err != nil {
		return PeerConnection{}, err
	}
	peer := PeerConnection{
		conn:                conn,
		peerId:              peerId,
		stream:              *stream,
		TorrentInfo:         info,
		MetadataExtensionId: -1,
	}

	if info == nil {
		req := benserde.BencodeDict(map[string]benserde.BencodeValue{
			"m": benserde.BencodeDict(
				map[string]benserde.BencodeValue{
					"ut_metadata": benserde.BencodeInt(1),
					"ut_pex":      benserde.BencodeInt(2),
				},
			),
		})
		err := peer.sendMessage(Extension, append([]byte{0}, benserde.EncodeBencode(req)...))
		if err != nil {
			return PeerConnection{}, err
		}
		err = peer.waitForMessage(Extension)
		if err != nil {
			return PeerConnection{}, err
		}
		_, extensionHandshakeDecode, err := benserde.DecodeBencode(peer.backlog[len(peer.backlog)-1].msgData[1:])
		if err != nil {
			return PeerConnection{}, err
		}
		extensionHandshake, err := extensionHandshakeDecode.AsDict()
		if err != nil {
			return PeerConnection{}, err
		}
		metadata, err := extensionHandshake["m"].AsDict()
		if err != nil {
			return PeerConnection{}, err
		}
		extId, err := metadata["ut_metadata"].AsInt()
		if err != nil {
			return PeerConnection{}, err
		}
		peer.MetadataExtensionId = extId
	}
	return peer, nil
}

func (p *PeerConnection) sendMessage(msgType MessageType, bytes []byte) error {
	lenBuffer := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuffer, uint32(len(bytes)+1))
	_, err := p.stream.Write(lenBuffer)
	if err != nil {
		return err
	}
	err = p.stream.WriteByte(byte(msgType))
	if err != nil {
		return err
	}
	_, err = p.stream.Write(bytes)
	if err != nil {
		return err
	}
	err = p.stream.Flush()
	if err != nil {
		return err
	}
	return nil
}

func (p *PeerConnection) waitForMessage(msgType MessageType) error {
	for {
		lengthBytes := make([]byte, 4)
		_, err := p.stream.Read(lengthBytes)
		if err != nil {
			return err
		}
		btPayload := make([]byte, binary.BigEndian.Uint32(lengthBytes))
		//_, err = p.stream.Read(btPayload)
		_, err = io.ReadAtLeast(p.stream.Reader, btPayload, len(btPayload))
		if err != nil {
			return err
		}

		peerMsg := PeerMessage{
			msgType: MessageType(btPayload[0]),
		}

		if len(btPayload) > 1 {
			peerMsg.msgData = btPayload[1:]
		}

		p.backlog = append(p.backlog, peerMsg)

		if btPayload[0] == byte(msgType) {
			break
		}
	}
	return nil
}

func (p *PeerConnection) GetMetadata() error {
	// todo: metadata in parts...

	req := benserde.BencodeDict(map[string]benserde.BencodeValue{
		"msg_type": benserde.BencodeInt(0),
		"piece":    benserde.BencodeInt(0),
	})
	err := p.sendMessage(Extension, append([]byte{byte(p.MetadataExtensionId)}, benserde.EncodeBencode(req)...))
	if err != nil {
		return err
	}
	err = p.waitForMessage(Extension)
	if err != nil {
		return err
	}
	extensionPayload := p.backlog[len(p.backlog)-1].msgData[1:]
	infoOffset, _, err := benserde.DecodeBencode(extensionPayload)
	if err != nil {
		return err
	}
	_, infoDecode, err := benserde.DecodeBencode(extensionPayload[infoOffset:])
	if err != nil {
		return err
	}
	info, err := infoDecode.AsDict()
	if err != nil {
		return err
	}
	p.TorrentInfo = info
	return nil
}

func (p *PeerConnection) DownloadPiece(pieceIndex int) ([]byte, error) {
	const BlockSize = 1024 * 16
	if !p.unchoked {
		err := p.showInterest()
		if err != nil {
			return []byte{}, err
		}
	}
	fileLen, err := p.TorrentInfo["length"].AsInt()
	pieceLen, err := p.TorrentInfo["piece length"].AsInt()
	pieceBuf := make([]byte, min(fileLen-(pieceIndex*pieceLen), pieceLen))
	downloadedPiece := 0
	for downloadedPiece < len(pieceBuf) {
		requestBuf := make([]byte, 12)
		binary.BigEndian.PutUint32(requestBuf, uint32(pieceIndex))
		binary.BigEndian.PutUint32(requestBuf[4:], uint32(downloadedPiece))
		binary.BigEndian.PutUint32(requestBuf[8:], min(BlockSize, uint32(len(pieceBuf)-downloadedPiece)))
		err := p.sendMessage(Request, requestBuf)
		if err != nil {
			return []byte{}, err
		}
		err = p.waitForMessage(Piece)
		if err != nil {
			return []byte{}, err
		}
		downloadedData := p.backlog[len(p.backlog)-1].msgData[8:]
		copy(pieceBuf[downloadedPiece:], downloadedData)
		downloadedPiece += len(downloadedData)
	}
	return pieceBuf, err
}

func (p *PeerConnection) showInterest() error {
	err := p.sendMessage(Interested, nil)
	if err != nil {
		return err
	}

	err = p.waitForMessage(Unchoke)
	if err != nil {
		return err
	}
	p.unchoked = true

	return nil
}
