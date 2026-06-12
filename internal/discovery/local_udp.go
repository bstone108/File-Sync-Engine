package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"
)

const localAnnouncementType = "fse.local.announcement.v1"

const DefaultLocalDiscoveryAddress = "255.255.255.255:22426"

type LocalAnnouncement struct {
	NodeID    string   `json:"nodeID"`
	Addresses []string `json:"addresses"`
}

type localAnnouncementEnvelope struct {
	Type         string            `json:"type"`
	Announcement LocalAnnouncement `json:"announcement"`
}

func EncodeLocalAnnouncement(announcement LocalAnnouncement) ([]byte, error) {
	if announcement.NodeID == "" {
		return nil, errors.New("local announcement nodeID is required")
	}
	if len(announcement.Addresses) == 0 {
		return nil, errors.New("local announcement addresses are required")
	}
	for i, address := range announcement.Addresses {
		if address == "" {
			return nil, fmt.Errorf("local announcement address[%d] is required", i)
		}
	}
	return json.Marshal(localAnnouncementEnvelope{Type: localAnnouncementType, Announcement: announcement})
}

func DecodeLocalAnnouncement(data []byte) (LocalAnnouncement, error) {
	var envelope localAnnouncementEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return LocalAnnouncement{}, err
	}
	if envelope.Type != localAnnouncementType {
		return LocalAnnouncement{}, fmt.Errorf("unexpected local announcement type %q", envelope.Type)
	}
	if envelope.Announcement.NodeID == "" {
		return LocalAnnouncement{}, errors.New("local announcement nodeID is required")
	}
	if len(envelope.Announcement.Addresses) == 0 {
		return LocalAnnouncement{}, errors.New("local announcement addresses are required")
	}
	for i, address := range envelope.Announcement.Addresses {
		if address == "" {
			return LocalAnnouncement{}, fmt.Errorf("local announcement address[%d] is required", i)
		}
	}
	return envelope.Announcement, nil
}

type LocalUDPSource struct {
	Conn    net.PacketConn
	Target  net.Addr
	Self    LocalAnnouncement
	Timeout time.Duration
}

func NewLocalUDPSource(listenAddr string, targetAddr string, self LocalAnnouncement, timeout time.Duration) (*LocalUDPSource, error) {
	if listenAddr == "" {
		listenAddr = ":22426"
	}
	if targetAddr == "" {
		targetAddr = DefaultLocalDiscoveryAddress
	}
	target, err := net.ResolveUDPAddr("udp4", targetAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenPacket("udp4", listenAddr)
	if err != nil {
		return nil, err
	}
	return &LocalUDPSource{Conn: conn, Target: target, Self: self, Timeout: timeout}, nil
}

func (s LocalUDPSource) Close() error {
	if s.Conn == nil {
		return nil
	}
	return s.Conn.Close()
}

func (s LocalUDPSource) Peers() ([]Peer, error) {
	return s.PeersContext(context.Background())
}

func (s LocalUDPSource) PeersContext(ctx context.Context) ([]Peer, error) {
	if s.Conn == nil {
		return nil, errors.New("local discovery connection is required")
	}
	if s.Target == nil {
		return nil, errors.New("local discovery target is required")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	announce, err := EncodeLocalAnnouncement(s.Self)
	if err != nil {
		return nil, err
	}
	if _, err := s.Conn.WriteTo(announce, s.Target); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	if setter, ok := s.Conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		if err := setter.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
	}

	peers := map[string]map[string]struct{}{}
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return sortedPeers(peers), err
		}
		n, _, err := s.Conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return sortedPeers(peers), nil
			}
			return sortedPeers(peers), err
		}
		announcement, err := DecodeLocalAnnouncement(buf[:n])
		if err != nil {
			continue
		}
		if announcement.NodeID == s.Self.NodeID {
			continue
		}
		addresses := peers[announcement.NodeID]
		if addresses == nil {
			addresses = map[string]struct{}{}
			peers[announcement.NodeID] = addresses
		}
		for _, address := range announcement.Addresses {
			addresses[address] = struct{}{}
		}
	}
}

func sortedPeers(peerAddresses map[string]map[string]struct{}) []Peer {
	ids := make([]string, 0, len(peerAddresses))
	for id := range peerAddresses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	peers := make([]Peer, 0, len(ids))
	for _, id := range ids {
		addresses := make([]string, 0, len(peerAddresses[id]))
		for address := range peerAddresses[id] {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		peers = append(peers, Peer{ID: id, Addresses: addresses})
	}
	return peers
}
