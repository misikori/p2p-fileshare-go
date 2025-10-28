package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"p2p-fileshare/metafile"
	pb "p2p-fileshare/tracker"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var peerID [PeerIDLen]byte

var ourPort int32

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
		go connectToPeer(peerAddr, meta.InfoHash, ourBitfield)
	}
	go startAnnounceHeartbeat(trackerClient, meta)

	log.Printf("Starting TCP listener on port %d", ourPort)
	go startTCPListener(meta.InfoHash, ourBitfield)
	log.Println("Client is running. Press Ctrl+C to exit.")
	select {}
}

func startTCPListener(infoHash []byte, bitfield Bitfield) {
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
		go handlePeerConnection(conn, infoHash, bitfield)
	}
}

func connectToPeer(addr string, infoHash []byte, bitfield Bitfield) {
	log.Printf("Connecting to peer: %s", addr)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
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

	if _, err := io.Copy(io.Discard, conn); err != nil {
		log.Printf("Connection with %s closed: %v", addr, err)
	}
}

func handlePeerConnection(conn net.Conn, infoHash []byte, bitfield Bitfield) {
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

	if _, err := io.Copy(io.Discard, conn); err != nil {
		log.Printf("Connection with %s closed: %v", conn.RemoteAddr(), err)
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
