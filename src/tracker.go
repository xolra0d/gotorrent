package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/netip"
	"strings"
	"time"
)

const PROTOCOL_ID int64 = 0x41727101980

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

const MAX_RETRIES = 8
const CONNECT_REQUEST_LENGTH = 4 + 4 + 8
const CONNECT_RESPONSE_LENGTH = 4 + 4 + 8
const ANNOUNCE_REQUEST_LENGTH = 98
const ANNOUNCE_RESPONSE_LENGTH_MIN = 4 + 4 + 4 + 4 + 4

func RandomTransactionId() int32 {
	return rand.Int31()
}

func RandomPeerId() [20]byte {
	peer_id := [20]byte{'G', 'T'}
	rand.Read(peer_id[2:])
	return peer_id
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
		} else {
			return net.IPv6len, port
		}
	case *net.TCPAddr:
		ip := v.IP
		port := v.Port

		if ip.To4() != nil {
			return net.IPv4len, port
		} else {
			return net.IPv6len, port
		}
	default:
		panic(fmt.Sprintf("Unknown addr type: %T. Allowed types are `net.UDPAddr`, `net.TCPAddr`.", v))
	}
}

func GenerateConnectBytes(transaction_id int32) []byte {
	// FORMAT
	// Offset  Size            Name            Value
	// 0       64-bit integer  protocol_id     0x41727101980 // magic constant
	// 8       32-bit integer  action          0 // connect
	// 12      32-bit integer  transaction_id
	result := make([]byte, 0, CONNECT_REQUEST_LENGTH)
	result = binary.BigEndian.AppendUint64(result, uint64(PROTOCOL_ID))
	result = binary.BigEndian.AppendUint32(result, uint32(CONNECT))
	result = binary.BigEndian.AppendUint32(result, uint32(transaction_id))
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

	result := make([]byte, 0, ANNOUNCE_REQUEST_LENGTH)
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

func DecodeConnectResponse(buffer []byte, transaction_id int32) (int64, error) {
	// FORMAT
	// Offset  Size            Name            Value
	// 0       32-bit integer  action          0 // connect
	// 4       32-bit integer  transaction_id
	// 8       64-bit integer  connection_id
	if len(buffer) != CONNECT_RESPONSE_LENGTH {
		return 0, fmt.Errorf("Expected to receive buffer of length `%v`, got `%v` instead during connect.", CONNECT_RESPONSE_LENGTH, len(buffer))
	}

	recv_transaction_id := binary.BigEndian.Uint32(buffer[4:])
	if recv_transaction_id != uint32(transaction_id) {
		return 0, fmt.Errorf("Expected to receive `%v` for transaction id in connection response, got `%v` instead.", transaction_id, recv_transaction_id)
	}

	action := ActionType(binary.BigEndian.Uint32(buffer))
	switch action {
	case CONNECT:
		connection_id := int64(binary.BigEndian.Uint64(buffer[8:]))
		return connection_id, nil
	case ERROR:
		return 0, RetryableError(string(buffer[8:]))
	default:
		return 0, fmt.Errorf("Expected to receive `CONNECT (%v)` for action in connection response, got `%v` instead.", uint32(CONNECT), action)
	}
}

func DecodeAnnounceResponse(buffer []byte, transaction_id int32, peer_count, ip_length int) (uint32, []netip.AddrPort, error) {
	// FORMAT
	// Offset      Size            Name            Value
	// 0           32-bit integer  action          1 // announce
	// 4           32-bit integer  transaction_id
	// 8           32-bit integer  interval
	// 12          32-bit integer  leechers
	// 16          32-bit integer  seeders
	// 20 + 6 * n  32-bit integer  IP address
	// 24 + 6 * n  16-bit integer  TCP port
	if len(buffer) != ANNOUNCE_RESPONSE_LENGTH_MIN+(ip_length+2)*peer_count {
		return 0, []netip.AddrPort{}, fmt.Errorf("Expected to receive buffer of length `%v`, got `%v` instead during announce.", ANNOUNCE_RESPONSE_LENGTH_MIN+(ip_length+2)*peer_count, len(buffer))
	}

	recv_transaction_id := binary.BigEndian.Uint32(buffer[4:])
	if recv_transaction_id != uint32(transaction_id) {
		return 0, []netip.AddrPort{}, fmt.Errorf("Expected to receive `%v` for transaction id in connection response, got `%v` instead.", transaction_id, recv_transaction_id)
	}

	action := ActionType(binary.BigEndian.Uint32(buffer))
	switch action {
	case ANNOUNCE:
		interval := binary.BigEndian.Uint32(buffer[8:])
		peers := make([]netip.AddrPort, 0, peer_count)
		for index := range peer_count {
			start := ANNOUNCE_RESPONSE_LENGTH_MIN + index*(ip_length+2)
			ip, ok := netip.AddrFromSlice(buffer[start : start+ip_length])
			if !ok {
				return 0, []netip.AddrPort{}, fmt.Errorf("Could not convert ip (%v) into IP addr.", buffer[start:start+ip_length])
			}
			port := binary.BigEndian.Uint16(buffer[start+ip_length : start+ip_length+2])
			peers = append(peers, netip.AddrPortFrom(ip, port))
		}
		return interval, peers, nil
	case ERROR:
		return 0, []netip.AddrPort{}, RetryableError(string(buffer[8:]))
	default:
		return 0, []netip.AddrPort{}, fmt.Errorf("Expected to receive `CONNECT (%v)` for action in connection response, got `%v` instead.", uint32(CONNECT), action)
	}

}

type TrackerConnection struct {
	handle    net.Conn
	ip_length int
	port      uint16

	interval       uint32
	transaction_id int32
	connection_id  int64
	peer_id        [20]byte
	downloaded     int64
	left           int64
	uploaded       int64 // planning to be 0
	key            uint32
}

func newTrackerConnection(ctx context.Context, tracker_socket string, peer_id [20]byte) (TrackerConnection, error) {
	if !strings.HasPrefix(tracker_socket, "udp://") {
		return TrackerConnection{}, fmt.Errorf("Currently support only UDP trackers.")
	}
	tracker_socket = tracker_socket[6:]
	if strings.HasSuffix(tracker_socket, "/announce") {
		tracker_socket = tracker_socket[:len(tracker_socket)-9]
	}

	var d net.Dialer
	handle, err := d.DialContext(ctx, "udp", tracker_socket)
	if err != nil {
		return TrackerConnection{}, err
	}

	ip_length, port := getIPLengthAndPort(handle.LocalAddr())
	return TrackerConnection{handle: handle, ip_length: ip_length, port: uint16(port), peer_id: peer_id}, nil
}

func NewTrackerConnection(ctx context.Context, tracker_socket string, peer_id [20]byte) (TrackerConnection, error) {
	var last_err error

	for retry := range MAX_RETRIES {
		if ctx.Err() != nil {
			return TrackerConnection{}, ctx.Err()
		}

		retry_ctx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		conn, err := newTrackerConnection(retry_ctx, tracker_socket, peer_id)
		cancel()
		if err == nil {
			return conn, nil
		}

		last_err = err
	}
	return TrackerConnection{}, fmt.Errorf("Could not connect to tracker: %v", last_err.Error())
}

func (conn *TrackerConnection) intitate(ctx context.Context) error {
	new_transaction_id := RandomTransactionId()
	conn_bytes := GenerateConnectBytes(new_transaction_id)

	if deadline, ok := ctx.Deadline(); ok {
		conn.handle.SetDeadline(deadline)
		defer conn.handle.SetDeadline(time.Time{})
	}

	n, err := conn.handle.Write(conn_bytes)
	if err != nil {
		return err
	} else if n != CONNECT_REQUEST_LENGTH {
		return fmt.Errorf("Expected to send `%v` bytes to tracker, sent `%v` instead.", CONNECT_REQUEST_LENGTH, n)
	}
	response_buffer := make([]byte, CONNECT_RESPONSE_LENGTH)
	n, err = conn.handle.Read(response_buffer)
	if err != nil {
		return err
	} else if n != CONNECT_RESPONSE_LENGTH {
		return fmt.Errorf("Expected to receive `%v` bytes from tracker, received `%v` instead.", CONNECT_RESPONSE_LENGTH, n)
	}
	conn_id, err := DecodeConnectResponse(response_buffer, new_transaction_id)
	if err != nil {
		return err
	}

	conn.transaction_id = new_transaction_id
	conn.connection_id = conn_id

	return nil
}

func (conn *TrackerConnection) Initiate(ctx context.Context) error {
	var last_err error

	for retry := range MAX_RETRIES {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		retry_ctx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		err := conn.intitate(retry_ctx)
		cancel()
		if err == nil {
			return nil
		}

		last_err = err
	}
	return fmt.Errorf("Could not connect to tracker: %v", last_err.Error())
}

func (conn *TrackerConnection) announce(ctx context.Context, hash string, peers_count int) ([]netip.AddrPort, error) {
	hash_arr, err := hex.DecodeString(hash)
	if err != nil {
		return []netip.AddrPort{}, err
	}

	req := AnnounceRequest{conn.connection_id, conn.transaction_id, [20]byte(hash_arr), conn.peer_id, conn.downloaded, conn.left, conn.uploaded, NONE, conn.key, int32(peers_count), conn.port}

	if deadline, ok := ctx.Deadline(); ok {
		conn.handle.SetDeadline(deadline)
		defer conn.handle.SetDeadline(time.Time{})
	}

	ann_bytes := GenerateAnnounceBytes(req)
	n, err := conn.handle.Write(ann_bytes)
	if err != nil {
		return []netip.AddrPort{}, err
	} else if n != ANNOUNCE_REQUEST_LENGTH {
		return []netip.AddrPort{}, fmt.Errorf("Expected to send `%v` bytes to tracker, sent `%v` instead.", ANNOUNCE_REQUEST_LENGTH, n)
	}

	response_buffer := make([]byte, ANNOUNCE_RESPONSE_LENGTH_MIN+(conn.ip_length+2)*peers_count)
	n, err = conn.handle.Read(response_buffer)
	if err != nil {
		return []netip.AddrPort{}, err
	}

	interval, peers, err := DecodeAnnounceResponse(response_buffer, conn.transaction_id, peers_count, conn.ip_length)
	if err != nil {
		return []netip.AddrPort{}, err
	}
	conn.interval = interval
	return peers, nil
}

func (conn *TrackerConnection) Announce(ctx context.Context, hash string, peers_count int) ([]netip.AddrPort, error) {
	var last_err error

	for retry := range MAX_RETRIES {
		if ctx.Err() != nil {
			return []netip.AddrPort{}, ctx.Err()
		}

		retry_ctx, cancel := context.WithTimeout(ctx, getResponseTimeout(retry))
		peers, err := conn.announce(retry_ctx, hash, peers_count)
		cancel()
		if err == nil {
			return peers, nil
		}

		// Only retry on retryable errors
		if _, ok := err.(RetryableError); !ok {
			return []netip.AddrPort{}, err
		}

		last_err = err
	}
	return []netip.AddrPort{}, fmt.Errorf("Could not announce to tracker: %v", last_err.Error())
}

func GetPeers(ctx context.Context, tracker_socket string, peer_id [20]byte, hash string, peers_ch chan netip.AddrPort, trackers chan TrackerConnection) {
	conn, err := NewTrackerConnection(ctx, tracker_socket, peer_id)
	if err != nil {
		fmt.Printf("Could not connect to %v tracker: %v", tracker_socket, err)
		return
	}
	err = conn.Initiate(ctx)
	if err != nil {
		fmt.Printf("Could not intitate to %v tracker: %v", tracker_socket, err)
		return
	}
	peers, err := conn.Announce(ctx, hash, 20)
	if err != nil {
		fmt.Printf("Could not announce to %v tracker: %v", tracker_socket, err)
		return
	}

	for _, peer := range peers {
		peers_ch <- peer
	}
	trackers <- conn
}
