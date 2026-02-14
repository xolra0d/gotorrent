package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/netip"
	"strings"
	"time"
)

const ProtocolId int64 = 0x41727101980

type ActionType int32

const (
	CONNECT ActionType = iota
	ANNOUNCE
	SCRAPE
	ERROR
)

type EventType uint32

const (
	NONE EventType = iota
)

type RetryableError string

func (error RetryableError) Error() string {
	return string(error)
}

const MaxRetries = 8
const ConnectRequestLength = 4 + 4 + 8
const ConnectResponseLength = 4 + 4 + 8
const AnnounceRequestLength = 98
const AnnounceResponseLengthMin = 4 + 4 + 4 + 4 + 4

func RandomTransactionId() int32 {
	return rand.Int31()
}

func RandomPeerId() [20]byte {
	peerId := [20]byte{'G', 'T'}
	_, _ = rand.Read(peerId[2:])
	return peerId
}

func getResponseTimeout(retry int) time.Duration {
	return time.Second * time.Duration(15*int(math.Pow(float64(2), float64(retry))))
}

func getIPLengthAndPort(addr net.Addr) (int, int) {
	switch v := addr.(type) {
	case *net.UDPAddr:
		ip := v.IP
		port := v.Port

		if ip.To4() != nil {
			return net.IPv4len, port
		}

		return net.IPv6len, port
	case *net.TCPAddr:
		ip := v.IP
		port := v.Port

		if ip.To4() != nil {
			return net.IPv4len, port
		}

		return net.IPv6len, port
	default:
		panic(fmt.Sprintf("Unknown addr type: %T. Allowed types are `net.UDPAddr`, `net.TCPAddr`.", v))
	}
}

func GenerateConnectBytes(transactionId int32) []byte {
	// FORMAT
	// Offset  Size            Name            Value
	// 0       64-bit integer  protocol_id     0x41727101980 // magic constant
	// 8       32-bit integer  action          0 // connect
	// 12      32-bit integer  transaction_id
	result := make([]byte, 0, ConnectRequestLength)
	result = binary.BigEndian.AppendUint64(result, uint64(ProtocolId))
	result = binary.BigEndian.AppendUint32(result, uint32(CONNECT))
	result = binary.BigEndian.AppendUint32(result, uint32(transactionId))
	return result
}

type AnnounceRequest struct {
	ConnectionID  int64
	TransactionID int32
	Hash          [20]byte
	PeerID        [20]byte
	Downloaded    int64
	Left          int64
	Uploaded      int64
	Event         EventType
	Key           uint32
	NumWant       int32
	Port          uint16
}

func GenerateAnnounceBytes(req AnnounceRequest) []byte {
	// FORMAT
	// Offset  Size    Name    Value
	// 0       64-bit integer  connection_id
	// 8       32-bit integer  action          1 // announce
	// 12      32-bit integer  transaction_id
	// 16      20-byte string  info_hash
	// 36      20-byte string  peer_id
	// 56      64-bit integer  downloaded
	// 64      64-bit integer  left
	// 72      64-bit integer  uploaded
	// 80      32-bit integer  event           0 // 0: none; 1: completed; 2: started; 3: stopped
	// 84      32-bit integer  IP address      0 // default
	// 88      32-bit integer  key
	// 92      32-bit integer  num_want        -1 // default
	// 96      16-bit integer  port

	result := make([]byte, 0, AnnounceRequestLength)
	result = binary.BigEndian.AppendUint64(result, uint64(req.ConnectionID))
	result = binary.BigEndian.AppendUint32(result, uint32(ANNOUNCE))
	result = binary.BigEndian.AppendUint32(result, uint32(req.TransactionID))
	result = append(result, req.Hash[:]...)
	result = append(result, req.PeerID[:]...)
	result = binary.BigEndian.AppendUint64(result, uint64(req.Downloaded))
	result = binary.BigEndian.AppendUint64(result, uint64(req.Left))
	result = binary.BigEndian.AppendUint64(result, uint64(req.Uploaded))
	result = binary.BigEndian.AppendUint32(result, uint32(req.Event))
	result = binary.BigEndian.AppendUint32(result, 0) // will be set by server
	result = binary.BigEndian.AppendUint32(result, req.Key)
	result = binary.BigEndian.AppendUint32(result, uint32(req.NumWant))
	result = binary.BigEndian.AppendUint16(result, req.Port)
	return result
}

func DecodeConnectResponse(buffer []byte, transactionId int32) (int64, error) {
	// FORMAT
	// Offset  Size            Name            Value
	// 0       32-bit integer  action          0 // connect
	// 4       32-bit integer  transaction_id
	// 8       64-bit integer  connection_id
	if len(buffer) != ConnectResponseLength {
		return 0, fmt.Errorf("expected to receive buffer of length `%v`, got `%v` instead during connect", ConnectResponseLength, len(buffer))
	}

	recvTransactionId := binary.BigEndian.Uint32(buffer[4:])
	if recvTransactionId != uint32(transactionId) {
		return 0, fmt.Errorf("expected to receive `%v` for transaction id in connection response, got `%v` instead", transactionId, recvTransactionId)
	}

	action := ActionType(binary.BigEndian.Uint32(buffer))
	switch action {
	case CONNECT:
		connectionId := int64(binary.BigEndian.Uint64(buffer[8:]))
		return connectionId, nil
	case ERROR:
		return 0, RetryableError(buffer[8:])
	default:
		return 0, fmt.Errorf("expected to receive `CONNECT (%v)` for action in connection response, got `%v` instead", uint32(CONNECT), action)
	}
}

func DecodeAnnounceResponse(buffer []byte, transactionId int32, peerCount, ipLength int) (uint32, []netip.AddrPort, error) {
	// FORMAT
	// Offset      Size            Name            Value
	// 0           32-bit integer  action          1 // announce
	// 4           32-bit integer  transaction_id
	// 8           32-bit integer  interval
	// 12          32-bit integer  leechers
	// 16          32-bit integer  seeders
	// 20 + 6 * n  32-bit integer  IP address
	// 24 + 6 * n  16-bit integer  TCP port
	if len(buffer) != AnnounceResponseLengthMin+(ipLength+2)*peerCount {
		return 0, []netip.AddrPort{}, fmt.Errorf("expected to receive buffer of length `%v`, got `%v` instead during announce", AnnounceResponseLengthMin+(ipLength+2)*peerCount, len(buffer))
	}

	recvTransactionId := binary.BigEndian.Uint32(buffer[4:])
	if recvTransactionId != uint32(transactionId) {
		return 0, []netip.AddrPort{}, fmt.Errorf("expected to receive `%v` for transaction id in connection response, got `%v` instead", transactionId, recvTransactionId)
	}

	action := ActionType(binary.BigEndian.Uint32(buffer))
	switch action {
	case ANNOUNCE:
		interval := binary.BigEndian.Uint32(buffer[8:])
		peers := make([]netip.AddrPort, 0, peerCount)
		for index := range peerCount {
			start := AnnounceResponseLengthMin + index*(ipLength+2)
			ip, ok := netip.AddrFromSlice(buffer[start : start+ipLength])
			if !ok {
				return 0, []netip.AddrPort{}, fmt.Errorf("could not convert ip (%v) into IP addr", buffer[start:start+ipLength])
			}
			port := binary.BigEndian.Uint16(buffer[start+ipLength : start+ipLength+2])

			if ip.IsUnspecified() { // no more peers
				return interval, peers, nil
			}

			peers = append(peers, netip.AddrPortFrom(ip, port))
		}
		return interval, peers, nil
	case ERROR:
		return 0, []netip.AddrPort{}, RetryableError(buffer[8:])
	default:
		return 0, []netip.AddrPort{}, fmt.Errorf("expected to receive `CONNECT (%v)` for action in connection response, got `%v` instead", uint32(CONNECT), action)
	}

}

type TrackerConnection struct {
	handle   net.Conn
	ipLength int
	port     uint16

	interval      uint32
	transactionId int32
	connectionId  int64
	peerId        [20]byte
	downloaded    int64
	left          int64
	uploaded      int64 // planning to be 0
	key           uint32
}

func newTrackerConnection(ctx context.Context, trackerSocket string, peerId [20]byte) (TrackerConnection, error) {
	if !strings.HasPrefix(trackerSocket, "udp://") {
		return TrackerConnection{}, fmt.Errorf("currently support only UDP trackers")
	}
	trackerSocket = trackerSocket[6:]
	if strings.HasSuffix(trackerSocket, "/announce") {
		trackerSocket = trackerSocket[:len(trackerSocket)-9]
	}

	var d net.Dialer
	handle, err := d.DialContext(ctx, "udp", trackerSocket)
	if err != nil {
		return TrackerConnection{}, err
	}

	ipLength, port := getIPLengthAndPort(handle.LocalAddr())
	return TrackerConnection{handle: handle, ipLength: ipLength, port: uint16(port), peerId: peerId}, nil
}

func NewTrackerConnection(ctx context.Context, trackerSocket string, peerId [20]byte) (TrackerConnection, error) {
	var lastErr error

	for retry := range MaxRetries {
		if ctx.Err() != nil {
			return TrackerConnection{}, ctx.Err()
		}

		retryCtx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		conn, err := newTrackerConnection(retryCtx, trackerSocket, peerId)
		cancel()
		if err == nil {
			return conn, nil
		}

		lastErr = err
	}
	return TrackerConnection{}, fmt.Errorf("could not connect to tracker: %v", lastErr)
}

func (conn *TrackerConnection) intimate(ctx context.Context) error {
	newTransactionId := RandomTransactionId()
	connBytes := GenerateConnectBytes(newTransactionId)

	if deadline, ok := ctx.Deadline(); ok {
		err := conn.handle.SetDeadline(deadline)
		if err != nil {
			return err
		}
		defer conn.handle.SetDeadline(time.Time{})
	}

	n, err := conn.handle.Write(connBytes)
	if err != nil {
		return err
	} else if n != ConnectRequestLength {
		return fmt.Errorf("expected to send `%v` bytes to tracker, sent `%v` instead", ConnectRequestLength, n)
	}
	responseBuffer := make([]byte, ConnectResponseLength)
	n, err = conn.handle.Read(responseBuffer)
	if err != nil {
		return err
	} else if n != ConnectResponseLength {
		return fmt.Errorf("expected to receive `%v` bytes from tracker, received `%v` instead", ConnectResponseLength, n)
	}
	connId, err := DecodeConnectResponse(responseBuffer, newTransactionId)
	if err != nil {
		return err
	}

	conn.transactionId = newTransactionId
	conn.connectionId = connId

	return nil
}

func (conn *TrackerConnection) Initiate(ctx context.Context) error {
	var lastErr error

	for retry := range MaxRetries {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		retryCtx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		err := conn.intimate(retryCtx)
		cancel()
		if err == nil {
			return nil
		}

		lastErr = err
	}
	return fmt.Errorf("could not connect to tracker: %v", lastErr)
}

func (conn *TrackerConnection) announce(ctx context.Context, hash string, peersCount int) ([]netip.AddrPort, error) {
	hashArr, err := hex.DecodeString(hash)
	if err != nil {
		return []netip.AddrPort{}, err
	}

	req := AnnounceRequest{conn.connectionId, conn.transactionId, [20]byte(hashArr), conn.peerId, conn.downloaded, conn.left, conn.uploaded, NONE, conn.key, int32(peersCount), conn.port}

	if deadline, ok := ctx.Deadline(); ok {
		err := conn.handle.SetDeadline(deadline)
		if err != nil {
			return nil, err
		}
		defer conn.handle.SetDeadline(time.Time{})
	}

	annBytes := GenerateAnnounceBytes(req)
	n, err := conn.handle.Write(annBytes)
	if err != nil {
		return []netip.AddrPort{}, err
	} else if n != AnnounceRequestLength {
		return []netip.AddrPort{}, fmt.Errorf("expected to send `%v` bytes to tracker, sent `%v` instead", AnnounceRequestLength, n)
	}

	responseBuffer := make([]byte, AnnounceResponseLengthMin+(conn.ipLength+2)*peersCount)
	n, err = conn.handle.Read(responseBuffer)
	if err != nil {
		return []netip.AddrPort{}, err
	}

	interval, peers, err := DecodeAnnounceResponse(responseBuffer, conn.transactionId, peersCount, conn.ipLength)
	if err != nil {
		return []netip.AddrPort{}, err
	}
	conn.interval = interval
	return peers, nil
}

func (conn *TrackerConnection) Announce(ctx context.Context, hash string, peersCount int) ([]netip.AddrPort, error) {
	var lastErr error

	for retry := range MaxRetries {
		if ctx.Err() != nil {
			return []netip.AddrPort{}, ctx.Err()
		}

		retryCtx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		peers, err := conn.announce(retryCtx, hash, peersCount)
		cancel()
		if err == nil {
			return peers, nil
		}

		// Only retry on retryable errors
		var retryableError RetryableError
		if !errors.As(err, &retryableError) {
			return []netip.AddrPort{}, err
		}

		lastErr = err
	}
	return []netip.AddrPort{}, fmt.Errorf("could not announce to tracker: %v", lastErr)
}

func GetPeers(ctx context.Context, trackerSocket string, peerId [20]byte, hash string, peersCh chan netip.AddrPort, trackers chan TrackerConnection) {
	conn, err := NewTrackerConnection(ctx, trackerSocket, peerId)
	if err != nil {
		fmt.Printf("Could not connect to %v tracker: %v\n", trackerSocket, err)
		return
	}
	err = conn.Initiate(ctx)
	if err != nil {
		fmt.Printf("Could not intimate to %v tracker: %v\n", trackerSocket, err)
		return
	}
	peers, err := conn.Announce(ctx, hash, 100)
	if err != nil {
		fmt.Printf("Could not announce to %v tracker: %v\n", trackerSocket, err)
		return
	}
	for _, peer := range peers {
		peersCh <- peer
	}
	trackers <- conn
}
