package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"p2p-fileshare/downloader"
	"p2p-fileshare/metafile"
	pb "p2p-fileshare/tracker"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var peerID [PeerIDLen]byte

var ourPort int32

var downloadManager *DownloadManager

// fixIPv6Address wraps IPv6 addresses in brackets if needed
// e.g., ::1:8080 -> [::1]:8080
func fixIPv6Address(addr string) string {
	// If the address already has brackets, return as is
	if strings.HasPrefix(addr, "[") {
		return addr
	}

	// Count colons - if more than 1, it's likely IPv6
	colonCount := strings.Count(addr, ":")
	if colonCount <= 1 {
		// IPv4 or hostname:port - return as is
		return addr
	}

	// It's IPv6 - need to wrap the IP part in brackets
	// Find the last colon (separates IP from port)
	lastColon := strings.LastIndex(addr, ":")
	if lastColon == -1 {
		return addr
	}

	ipPart := addr[:lastColon]
	portPart := addr[lastColon+1:]

	return fmt.Sprintf("[%s]:%s", ipPart, portPart)
}

func init() {
	if _, err := rand.Read(peerID[:]); err != nil {
		log.Fatalf("Failed to generate peer ID: %v", err)
	}

	// Find an available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("Failed to find available port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	ourPort = int32(addr.Port)
	listener.Close()
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run . <meta_file_path>")
	}
	metaFilePath := os.Args[1]

	meta, err := metafile.Parse(metaFilePath)

	if err != nil {
		log.Fatalf("Failed to parse meta file: %v", err)
	}

	numPieces := len(meta.Info.Hashes)
	ourBitfield := NewBitfield(numPieces)

	// Check if we already have the file and initialize bitfield accordingly
	outputPath := filepath.Join("./downloads", meta.Info.Name)
	if fileInfo, err := os.Stat(outputPath); err == nil && fileInfo.Size() == meta.Info.Size {
		// File exists and has correct size - mark all pieces as available
		log.Printf("File already exists locally, marking all pieces as available")
		for i := 0; i < numPieces; i++ {
			ourBitfield.Set(i)
		}
	}

	// Initialize the downloader
	// Create downloads directory if it doesn't exist
	if err := os.MkdirAll("./downloads", 0755); err != nil {
		log.Fatalf("Failed to create downloads directory: %v", err)
	}

	fileDownloader, err := downloader.NewFileDownloader(meta, outputPath)
	if err != nil {
		log.Fatalf("Failed to create file downloader: %v", err)
	}
	defer fileDownloader.Close()

	// Create the download manager with our bitfield
	downloadManager = NewDownloadManager(fileDownloader, ourBitfield)

	// Start the download manager
	downloadManager.Start()

	// Start processing incoming pieces
	go downloadManager.ProcessIncomingPieces()

	parsedURL, err := url.Parse(meta.TrackerURL)
	if err != nil {
		log.Fatalf("Invalid tracker URL: %v", err)
	}

	conn, err := grpc.NewClient(parsedURL.Host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Did not connect to tracker: %v", err)
	}
	defer conn.Close()

	trackerClient := pb.NewTrackerClient(conn)

	infoHashHex := hex.EncodeToString(meta.InfoHash)
	log.Printf("Requesting peers for info hash: %s", infoHashHex)

	getPeersReq := &pb.GetPeersRequest{InfoHash: meta.InfoHash}
	getPeersRes, err := trackerClient.GetPeers(context.Background(), getPeersReq)
	if err != nil {
		log.Fatalf("Could not get peers: %v", err)
	}
	log.Printf("Got peers: %v", getPeersRes.GetPeers())

	// for each peer, spawn a goroutine to connect and handshake
	for _, peerAddr := range getPeersRes.GetPeers() {
		go connectToPeer(peerAddr, meta.InfoHash, ourBitfield, meta.Info)
	}
	go startAnnounceHeartbeat(trackerClient, meta)

	log.Printf("Starting TCP listener on port %d", ourPort)
	go startTCPListener(meta.InfoHash, ourBitfield, meta.Info)
	log.Println("Client is running. Press Ctrl+C to exit.")
	select {}
}

func startTCPListener(infoHash []byte, bitfield Bitfield, fileInfo metafile.FileInfo) {
	addr := fmt.Sprintf(":%d", ourPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to open TCP listener: %v", err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Printf("New peer connected: %s", conn.RemoteAddr())
		go handlePeerConnection(conn, infoHash, bitfield, fileInfo)
	}
}

func connectToPeer(addr string, infoHash []byte, bitfield Bitfield, fileInfo metafile.FileInfo) {
	log.Printf("Connecting to peer: %s", addr)

	// Fix IPv6 addresses - need to be wrapped in brackets
	// e.g., ::1:8080 should be [::1]:8080
	dialAddr := fixIPv6Address(addr)

	conn, err := net.DialTimeout("tcp", dialAddr, 3*time.Second)
	if err != nil {
		log.Printf("Failed to dial peer %s: %v", addr, err)
		return
	}
	defer conn.Close()

	// 1 -> send our handshake
	ourHandshake := &Handshake{Pstr: ProtocolName}
	copy(ourHandshake.InfoHash[:], infoHash)
	copy(ourHandshake.PeerID[:], peerID[:])

	log.Printf("[%s] Sending handshake..", addr)
	if err := ourHandshake.Send(conn); err != nil {
		log.Printf("[%s] Failed to send handshake: %v", addr, err)
		return
	}
	log.Printf("[%s] Handshake sent.", addr)

	// 2 -> receive their handshake
	theirHandshake := &Handshake{}
	log.Printf("[%s] Waiting for handshake..", addr)
	if err := theirHandshake.Read(conn); err != nil {
		log.Printf("Failed to read handshake from %s: %v", addr, err)
		return
	}
	log.Printf("[%s] Handshake received.", addr)

	// 3 -> validate the handshake
	if !bytes.Equal(theirHandshake.InfoHash[:], infoHash) {
		log.Printf("Peer %s has incorrect infoHash. Dropping..", addr)
		return
	}

	log.Printf("Successfully handshaked with peer: %s", addr)

	log.Printf("[%s] Sending bitfield...", addr)
	msg := Message{Type: MsgBitfield, Payload: bitfield}
	if err := msg.Send(conn); err != nil {
		log.Printf("[%s] Failed to send bitfield: %v", addr, err)
		return
	}

	log.Printf("[%s] Waiting for bitfield..", addr)
	recvMsg, err := ReadMessage(conn)
	if err != nil {
		log.Printf("[%s] Failed to read bitfield: %v", addr, err)
		return
	}
	if recvMsg.Type != MsgBitfield {
		log.Printf("[%s] Expected bitfield, got %d", addr, recvMsg.Type)
		return
	}

	theirBitfield := Bitfield(recvMsg.Payload)
	log.Printf("[%s] Received bitfield. Peer has %d pieces.", addr, bytes.Count(theirBitfield, []byte{1}))

	// Create peer with download manager's channels
	var workQueue chan *Request
	var resultsQueue chan *Piece
	if downloadManager != nil {
		workQueue = make(chan *Request, 10) // Per-peer work queue
		resultsQueue = downloadManager.GetResultsQueue()
	}

	peer := NewPeer(conn, theirBitfield, fileInfo, workQueue, resultsQueue)

	// Register peer with download manager
	if downloadManager != nil {
		downloadManager.RegisterPeer(addr, peer)
		defer downloadManager.UnregisterPeer(addr)
	}

	if err := peer.RunMessageLoop(); err != nil {
		log.Printf("Peer %s disconnected: %v", addr, err)
	}

}

func handlePeerConnection(conn net.Conn, infoHash []byte, bitfield Bitfield, fileInfo metafile.FileInfo) {
	defer conn.Close()
	addr := conn.RemoteAddr().String()
	log.Printf("Handling connection from %s", addr)

	theirHandshake := &Handshake{}
	log.Printf("[%s] Waiting for handshake..", addr)
	if err := theirHandshake.Read(conn); err != nil {
		log.Printf("Failed to read handshake from %s: %v", conn.RemoteAddr(), err)
		return
	}

	if !bytes.Equal(theirHandshake.InfoHash[:], infoHash) {
		log.Printf("Peer %s has incorrect infoHash. Dropping..", conn.RemoteAddr())
		return
	}

	ourHandshake := &Handshake{Pstr: ProtocolName}
	copy(ourHandshake.InfoHash[:], infoHash)
	copy(ourHandshake.PeerID[:], peerID[:])

	log.Printf("[%s] Sending handshake..", addr)
	if err := ourHandshake.Send(conn); err != nil {
		log.Printf("Failed to send handshake to %s: %v", conn.RemoteAddr(), err)
		return
	}
	log.Printf("[%s] Handshake sent.", addr)

	log.Printf("Successfully handshaked with peer: %s", conn.RemoteAddr())

	// Receive their bitfield
	log.Printf("[%s] Waiting for bitfield...", addr)
	recvMsg, err := ReadMessage(conn)
	if err != nil {
		log.Printf("[%s] Failed to read bitfield: %v", addr, err)
		return
	}
	if recvMsg.Type != MsgBitfield {
		log.Printf("[%s] Expected bitfield, got %d", addr, recvMsg.Type)
		return
	}

	theirBitfield := Bitfield(recvMsg.Payload)
	log.Printf("[%s] Received bitfield. Peer has %d pieces.", addr, bytes.Count(theirBitfield, []byte{1})) // Simple check

	// Send our bitfield
	log.Printf("[%s] Sending bitfield...", addr)
	msg := Message{Type: MsgBitfield, Payload: bitfield}
	if err := msg.Send(conn); err != nil {
		log.Printf("[%s] Failed to send bitfield: %v", addr, err)
		return
	}

	log.Printf("[%s] Entering message loop...", addr)

	// Create peer with download manager's channels
	var workQueue chan *Request
	var resultsQueue chan *Piece
	if downloadManager != nil {
		workQueue = make(chan *Request, 10) // Per-peer work queue
		resultsQueue = downloadManager.GetResultsQueue()
	}

	peer := NewPeer(conn, theirBitfield, fileInfo, workQueue, resultsQueue)

	// Register peer with download manager
	if downloadManager != nil {
		downloadManager.RegisterPeer(addr, peer)
		defer downloadManager.UnregisterPeer(addr)
	}

	if err := peer.RunMessageLoop(); err != nil {
		log.Printf("Peer %s disconnected: %v", addr, err)
	}

}

func startAnnounceHeartbeat(client pb.TrackerClient, meta *metafile.MetaInfo) {
	interval := announce(client, meta.InfoHash, ourPort)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		interval = announce(client, meta.InfoHash, ourPort)
		ticker.Reset(time.Duration(interval) * time.Second)
	}
}

func announce(client pb.TrackerClient, infoHash []byte, port int32) int32 {
	log.Println("Announcing to tracker...")
	req := &pb.AnnounceRequest{
		InfoHash: infoHash,
		PeerId:   peerID[:],
		Port:     port,
	}

	res, err := client.Announce(context.Background(), req)
	if err != nil {
		log.Printf("Could not announce: %v", err)
		return 60
	}

	log.Printf("Announce OK. Next announce in %d seconds.", res.Interval)
	return res.Interval
}
