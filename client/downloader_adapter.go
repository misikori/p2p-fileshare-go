package main

import (
	"log"
	"p2p-fileshare/downloader"
	"sync"
	"time"
)

// DownloadManager bridges the downloader package with the client networking layer
type DownloadManager struct {
	fileDownloader *downloader.FileDownloader

	// Maps to track active peers
	activePeers      map[string]*Peer // peerAddr -> Peer
	activePeersMutex sync.RWMutex

	// Channels for communication with networker (client)
	clientWorkQueue    chan *Request
	clientResultsQueue chan *Piece

	// Bitfield to update when pieces are downloaded
	ourBitfield Bitfield

	// Shutdown channel
	done chan struct{}
}

// NewDownloadManager creates a new download manager
func NewDownloadManager(fd *downloader.FileDownloader, bitfield Bitfield) *DownloadManager {
	return &DownloadManager{
		fileDownloader:     fd,
		activePeers:        make(map[string]*Peer),
		clientWorkQueue:    make(chan *Request, 100),
		clientResultsQueue: make(chan *Piece, 100),
		ourBitfield:        bitfield,
		done:               make(chan struct{}),
	}
}

// RegisterPeer registers a peer for downloading
func (dm *DownloadManager) RegisterPeer(peerAddr string, peer *Peer) {
	dm.activePeersMutex.Lock()
	defer dm.activePeersMutex.Unlock()
	dm.activePeers[peerAddr] = peer
	log.Printf("[DownloadManager] Registered peer: %s", peerAddr)
}

// UnregisterPeer removes a peer
func (dm *DownloadManager) UnregisterPeer(peerAddr string) {
	dm.activePeersMutex.Lock()
	defer dm.activePeersMutex.Unlock()
	delete(dm.activePeers, peerAddr)
	log.Printf("[DownloadManager] Unregistered peer: %s", peerAddr)
}

// GetWorkQueue returns the work queue for client messages
func (dm *DownloadManager) GetWorkQueue() chan *Request {
	return dm.clientWorkQueue
}

// GetResultsQueue returns the results queue for client messages
func (dm *DownloadManager) GetResultsQueue() chan *Piece {
	return dm.clientResultsQueue
}

// Start starts the download manager's main loops
func (dm *DownloadManager) Start() {
	// Start the piece processor (handles incoming pieces from resultsQueue)
	go dm.processPieces()

	// Start the request distributor (converts downloader requests to client requests)
	go dm.distributeRequests()

	// Start the download strategy loop (decides what to request)
	go dm.downloadStrategy()

	// Start the progress monitor
	go dm.monitorProgress()
}

// processPieces converts client Piece messages to downloader PieceData and processes them
func (dm *DownloadManager) processPieces() {
	for {
		select {
		case piece := <-dm.clientResultsQueue:
			if piece == nil {
				return
			}

			// Convert client Piece to downloader PieceData
			pieceData := downloader.PieceData{
				PieceIndex: int(piece.Index),
				Begin:      int(piece.Begin),
				Data:       piece.Data,
			}

			// Send to downloader's results queue
			dm.fileDownloader.ResultsQueue <- pieceData

			// Update our bitfield asynchronously after piece is processed
			go func(pieceIndex int) {
				// Give the downloader time to verify and mark the piece
				time.Sleep(100 * time.Millisecond)

				// Check if the piece was successfully saved
				if dm.fileDownloader.GetPieceStatus(pieceIndex) == downloader.Have {
					if err := dm.ourBitfield.Set(pieceIndex); err == nil {
						log.Printf("Updated bitfield: now have piece %d", pieceIndex)
					}
				}
			}(int(piece.Index))

		case <-dm.done:
			return
		}
	}
}

// distributeRequests reads from downloader's work queue and converts to client requests
func (dm *DownloadManager) distributeRequests() {
	for {
		select {
		case pieceReq := <-dm.fileDownloader.WorkQueue:
			// Convert downloader PieceRequest to client Request
			clientReq := &Request{
				Index:  uint32(pieceReq.PieceIndex),
				Begin:  uint32(pieceReq.Begin),
				Length: uint32(pieceReq.Length),
			}

			// Find the peer by address and send the request to that peer's work queue
			dm.activePeersMutex.RLock()
			peer, exists := dm.activePeers[pieceReq.PeerAddr]
			dm.activePeersMutex.RUnlock()

			if exists && peer.workQueue != nil {
				select {
				case peer.workQueue <- clientReq:
					log.Printf("[DownloadManager] Sent request for piece %d to peer %s", pieceReq.PieceIndex, pieceReq.PeerAddr)
				default:
					log.Printf("[DownloadManager] Peer %s work queue full, dropping request for piece %d", pieceReq.PeerAddr, pieceReq.PieceIndex)
				}
			} else {
				log.Printf("[DownloadManager] Peer %s not found or has no work queue", pieceReq.PeerAddr)
			}

		case <-dm.done:
			return
		}
	}
}

// downloadStrategy is the "brain" that decides what pieces to request
func (dm *DownloadManager) downloadStrategy() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if download is complete
			if dm.fileDownloader.IsComplete() {
				log.Println("[DownloadManager] Download complete! All pieces downloaded and verified.")
				dm.fileDownloader.PrintStatus() // Print final status
				return
			}

			// Reset stale requests (pieces requested but not received after 30 seconds)
			resetPieces := dm.fileDownloader.ResetStaleRequests(30 * time.Second)
			if len(resetPieces) > 0 {
				log.Printf("[DownloadManager] Reset %d stale piece requests", len(resetPieces))
			}

			// Get list of active peers
			dm.activePeersMutex.RLock()
			var availablePeers []string
			for addr := range dm.activePeers {
				availablePeers = append(availablePeers, addr)
			}
			dm.activePeersMutex.RUnlock()

			if len(availablePeers) == 0 {
				continue // No peers available
			}

			// Request next pieces (max 5 concurrent requests)
			dm.fileDownloader.RequestNextPieces(availablePeers, 5)

		case <-dm.done:
			return
		}
	}
}

// monitorProgress periodically prints download progress
func (dm *DownloadManager) monitorProgress() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Stop printing status once download is complete
			if dm.fileDownloader.IsComplete() {
				log.Println("[DownloadManager] Download complete! Stopping progress monitor.")
				return
			}
			dm.fileDownloader.PrintStatus()

		case <-dm.done:
			return
		}
	}
}

// ProcessIncomingPieces starts processing pieces from the results queue
// This should be called in a goroutine
func (dm *DownloadManager) ProcessIncomingPieces() {
	dm.fileDownloader.ProcessPieceData()
}

// Stop gracefully shuts down the download manager
func (dm *DownloadManager) Stop() {
	close(dm.done)
}
