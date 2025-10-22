package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"p2p-fileshare/metafile"
	pb "p2p-fileshare/tracker"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run . <meta_file_path>")
	}
	metaFilePath := os.Args[1]

	meta, err := metafile.Parse(metaFilePath)
	if err != nil {
		log.Fatalf("Failed to parse meta file: %v", err)
	}

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

	go startAnnounceHeartbeat(trackerClient, meta)

	log.Println("Client is running. Press Ctrl+C to exit.")
	select {}
}

func startAnnounceHeartbeat(client pb.TrackerClient, meta *metafile.MetaInfo) {
	const ourPort = 6881
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
	peerID := fmt.Sprintf("peer-%d", time.Now().UnixNano())
	req := &pb.AnnounceRequest{
		InfoHash: infoHash,
		PeerId:   peerID,
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
