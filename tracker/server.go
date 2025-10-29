package tracker

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

const (
	cleanupInterval = 1 * time.Minute
	peerTimeout     = 2 * time.Minute
)

type peerInfo struct {
	lastSeen time.Time
}

type Tracker struct {
	UnimplementedTrackerServer
	mu       sync.Mutex
	torrents map[string]map[string]peerInfo
}

func NewTrackerServer() *Tracker {
	s := &Tracker{
		torrents: make(map[string]map[string]peerInfo),
	}
	go s.cleanupLoop()
	return s
}

func (s *Tracker) Announce(ctx context.Context, req *AnnounceRequest) (*AnnounceReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := peer.FromContext(ctx)
	if !ok || p == nil || p.Addr == nil {
		return nil, fmt.Errorf("unable to get peer address from context")
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return nil, fmt.Errorf("invalid peer address: %v", err)
	}

	peerAddr := fmt.Sprintf("%s:%d", host, req.Port)
	infoHashHex := hex.EncodeToString(req.InfoHash)

	if _, ok := s.torrents[infoHashHex]; !ok {
		s.torrents[infoHashHex] = make(map[string]peerInfo)
	}

	s.torrents[infoHashHex][peerAddr] = peerInfo{lastSeen: time.Now()}
	log.Printf("Announce from %s for %s", peerAddr, infoHashHex)

	return &AnnounceReply{Interval: int32(cleanupInterval.Seconds())}, nil
}

func (s *Tracker) GetPeers(ctx context.Context, req *GetPeersRequest) (*GetPeersReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	infoHashHex := hex.EncodeToString(req.InfoHash)
	peerMap, ok := s.torrents[infoHashHex]
	if !ok {
		return &GetPeersReply{Peers: []string{}}, nil
	}

	peers := make([]string, 0, len(peerMap))
	for peerAddr := range peerMap {
		peers = append(peers, peerAddr)
	}

	log.Printf("GetPeers for %s, returning %d peers", infoHashHex, len(peers))
	return &GetPeersReply{Peers: peers}, nil
}

func (s *Tracker) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		log.Println("Running cleanup...")
		for infoHash, peerMap := range s.torrents {
			for peerAddr, peerInfo := range peerMap {
				if time.Since(peerInfo.lastSeen) > peerTimeout {
					log.Printf("Removing stale peer %s for %s", peerAddr, infoHash)
					delete(peerMap, peerAddr)
				}
			}
			if len(peerMap) == 0 {
				delete(s.torrents, infoHash)
			}
		}
		s.mu.Unlock()
	}
}

func RunServer(port string) {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	RegisterTrackerServer(grpcServer, NewTrackerServer())

	log.Printf("Tracker server listening on %s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
