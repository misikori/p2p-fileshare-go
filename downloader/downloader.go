package downloader

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"

	"p2p-fileshare/metafile"
)

// PieceStatus represents the state of a file piece
type PieceStatus int

const (
	Needed PieceStatus = iota
	Requested
	Have
)

// String returns string representation of PieceStatus
func (ps PieceStatus) String() string {
	switch ps {
	case Needed:
		return "Needed"
	case Requested:
		return "Requested"
	case Have:
		return "Have"
	default:
		return "Unknown"
	}
}

// PieceRequest represents a request to download a piece from a peer
type PieceRequest struct {
	PieceIndex int
	PeerAddr   string
	Begin      int
	Length     int
}

// PieceData represents downloaded piece data from a peer
type PieceData struct {
	PieceIndex int
	Begin      int
	Data       []byte
	PeerAddr   string
}

// PieceState tracks the state of a single piece
type PieceState struct {
	Index         int
	Status        PieceStatus
	Hash          []byte
	RequestedAt   time.Time
	RequestedFrom string
}

// FileDownloader manages the download state of a file
type FileDownloader struct {
	metaInfo   *metafile.MetaInfo
	pieces     []PieceState
	outputFile *os.File
	mu         sync.RWMutex

	// Communication channels with the Networker
	WorkQueue    chan PieceRequest
	ResultsQueue chan PieceData

	// Statistics
	totalPieces     int
	completedPieces int
}

// NewFileDownloader creates a new file downloader instance
func NewFileDownloader(meta *metafile.MetaInfo, outputPath string) (*FileDownloader, error) {
	// Open/create the output file
	outputFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %v", err)
	}

	// Pre-allocate file to the correct size
	err = outputFile.Truncate(meta.Info.Size)
	if err != nil {
		outputFile.Close()
		return nil, fmt.Errorf("failed to allocate file space: %v", err)
	}

	totalPieces := len(meta.Info.Hashes)
	pieces := make([]PieceState, totalPieces)

	// Initialize all pieces as Needed
	for i := 0; i < totalPieces; i++ {
		pieces[i] = PieceState{
			Index:  i,
			Status: Needed,
			Hash:   meta.Info.Hashes[i],
		}
	}

	fd := &FileDownloader{
		metaInfo:        meta,
		pieces:          pieces,
		outputFile:      outputFile,
		totalPieces:     totalPieces,
		completedPieces: 0,
		WorkQueue:       make(chan PieceRequest, 100),
		ResultsQueue:    make(chan PieceData, 100),
	}

	return fd, nil
}

// Close closes the file downloader and cleans up resources
func (fd *FileDownloader) Close() error {
	close(fd.WorkQueue)
	close(fd.ResultsQueue)
	return fd.outputFile.Close()
}

// GetProgress returns download progress information
func (fd *FileDownloader) GetProgress() (completed, total int, percentage float64) {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	completed = fd.completedPieces
	total = fd.totalPieces
	if total > 0 {
		percentage = float64(completed) / float64(total) * 100
	}
	return
}

// IsComplete returns true if all pieces have been downloaded
func (fd *FileDownloader) IsComplete() bool {
	fd.mu.RLock()
	defer fd.mu.RUnlock()
	return fd.completedPieces == fd.totalPieces
}

// GetPieceStatus returns the status of a specific piece
func (fd *FileDownloader) GetPieceStatus(pieceIndex int) PieceStatus {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	if pieceIndex < 0 || pieceIndex >= len(fd.pieces) {
		return Needed // Invalid index
	}
	return fd.pieces[pieceIndex].Status
}

// GetFileInfo returns basic file information
func (fd *FileDownloader) GetFileInfo() (name string, size int64, chunkSize int64) {
	return fd.metaInfo.Info.Name, fd.metaInfo.Info.Size, fd.metaInfo.Info.ChunkSize
}

// VerifyPiece verifies a downloaded piece against its expected hash
// Returns true if the piece is valid, false otherwise
func (fd *FileDownloader) VerifyPiece(pieceData PieceData) bool {
	if pieceData.PieceIndex < 0 || pieceData.PieceIndex >= len(fd.pieces) {
		return false
	}

	// Calculate SHA-256 hash of the piece data
	hash := sha256.Sum256(pieceData.Data)
	expectedHash := fd.pieces[pieceData.PieceIndex].Hash

	// Compare with expected hash from metafile
	if len(expectedHash) != len(hash) {
		return false
	}

	for i := range expectedHash {
		if expectedHash[i] != hash[i] {
			return false
		}
	}

	return true
}

// WritePieceToDisk writes a verified piece to the correct position in the output file
func (fd *FileDownloader) WritePieceToDisk(pieceData PieceData) error {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	pieceIndex := pieceData.PieceIndex
	if pieceIndex < 0 || pieceIndex >= len(fd.pieces) {
		return fmt.Errorf("invalid piece index: %d", pieceIndex)
	}

	// Calculate file position: pieceIndex * chunkSize
	position := int64(pieceIndex) * fd.metaInfo.Info.ChunkSize

	// Seek to the correct position in the file
	_, err := fd.outputFile.Seek(position, 0)
	if err != nil {
		return fmt.Errorf("failed to seek to position %d: %v", position, err)
	}

	// Write the piece data
	_, err = fd.outputFile.Write(pieceData.Data)
	if err != nil {
		return fmt.Errorf("failed to write piece %d: %v", pieceIndex, err)
	}

	// Sync to ensure data is written to disk
	err = fd.outputFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync piece %d: %v", pieceIndex, err)
	}

	// Update piece status
	fd.pieces[pieceIndex].Status = Have
	fd.completedPieces++

	return nil
}

// GetNextPieceToRequest selects the next piece to download using sequential strategy
// Returns the piece index, or -1 if no pieces are needed
func (fd *FileDownloader) GetNextPieceToRequest() int {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	// Sequential strategy: find the first piece with status "Needed"
	for i, piece := range fd.pieces {
		if piece.Status == Needed {
			return i
		}
	}

	return -1 // No pieces needed
}

// MarkPieceAsRequested marks a piece as being requested from a peer
func (fd *FileDownloader) MarkPieceAsRequested(pieceIndex int, peerAddr string) error {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= len(fd.pieces) {
		return fmt.Errorf("invalid piece index: %d", pieceIndex)
	}

	if fd.pieces[pieceIndex].Status != Needed {
		return fmt.Errorf("piece %d is not in Needed state (current: %v)", pieceIndex, fd.pieces[pieceIndex].Status)
	}

	// Update piece state
	fd.pieces[pieceIndex].Status = Requested
	fd.pieces[pieceIndex].RequestedAt = time.Now()
	fd.pieces[pieceIndex].RequestedFrom = peerAddr

	return nil
}

// ResetStaleRequests resets pieces that have been requested but not received for too long
func (fd *FileDownloader) ResetStaleRequests(timeout time.Duration) []int {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	var resetPieces []int
	now := time.Now()

	for i, piece := range fd.pieces {
		if piece.Status == Requested && now.Sub(piece.RequestedAt) > timeout {
			fd.pieces[i].Status = Needed
			fd.pieces[i].RequestedAt = time.Time{}
			fd.pieces[i].RequestedFrom = ""
			resetPieces = append(resetPieces, i)
		}
	}

	return resetPieces
}

// GetAvailablePieces returns pieces that are available from a peer (based on their bitfield)
// This would be used with the peer's bitfield to find pieces we need that they have
func (fd *FileDownloader) GetAvailablePieces(peerBitfield []byte) []int {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	var availablePieces []int

	for i, piece := range fd.pieces {
		if piece.Status == Needed {
			// Check if the peer has this piece (bit is set in bitfield)
			byteIndex := i / 8
			bitIndex := i % 8

			if byteIndex < len(peerBitfield) {
				if (peerBitfield[byteIndex] & (1 << (7 - bitIndex))) != 0 {
					availablePieces = append(availablePieces, i)
				}
			}
		}
	}

	return availablePieces
}

// ProcessPieceData processes incoming piece data from the ResultsQueue
// This is the main function that should be called in a goroutine to handle downloaded pieces
func (fd *FileDownloader) ProcessPieceData() {
	for pieceData := range fd.ResultsQueue {
		// First, verify the piece
		if !fd.VerifyPiece(pieceData) {
			fmt.Printf("Piece %d failed verification, discarding\n", pieceData.PieceIndex)
			// Reset the piece status so it can be requested again
			fd.mu.Lock()
			if fd.pieces[pieceData.PieceIndex].Status == Requested {
				fd.pieces[pieceData.PieceIndex].Status = Needed
				fd.pieces[pieceData.PieceIndex].RequestedAt = time.Time{}
				fd.pieces[pieceData.PieceIndex].RequestedFrom = ""
			}
			fd.mu.Unlock()
			continue
		}

		// If verification passed, write to disk
		err := fd.WritePieceToDisk(pieceData)
		if err != nil {
			fmt.Printf("Failed to write piece %d to disk: %v\n", pieceData.PieceIndex, err)
			// Reset the piece status so it can be requested again
			fd.mu.Lock()
			if fd.pieces[pieceData.PieceIndex].Status == Requested {
				fd.pieces[pieceData.PieceIndex].Status = Needed
				fd.pieces[pieceData.PieceIndex].RequestedAt = time.Time{}
				fd.pieces[pieceData.PieceIndex].RequestedFrom = ""
			}
			fd.mu.Unlock()
			continue
		}

		fmt.Printf("Successfully downloaded and verified piece %d (%d/%d)\n",
			pieceData.PieceIndex, fd.completedPieces, fd.totalPieces)

		// Check if download is complete
		if fd.IsComplete() {
			fmt.Printf("Download complete! File saved with %d pieces.\n", fd.totalPieces)
			return
		}
	}
}

// RequestNextPieces finds pieces to request and sends them to the WorkQueue
// This function implements the download strategy
func (fd *FileDownloader) RequestNextPieces(availablePeers []string, maxConcurrentRequests int) {
	if len(availablePeers) == 0 {
		return
	}

	// Count current requested pieces
	fd.mu.RLock()
	requestedCount := 0
	for _, piece := range fd.pieces {
		if piece.Status == Requested {
			requestedCount++
		}
	}
	fd.mu.RUnlock()

	// Don't exceed maximum concurrent requests
	if requestedCount >= maxConcurrentRequests {
		return
	}

	// Find pieces we need
	piecesToRequest := maxConcurrentRequests - requestedCount
	peerIndex := 0

	for i := 0; i < piecesToRequest; i++ {
		pieceIndex := fd.GetNextPieceToRequest()
		if pieceIndex == -1 {
			break // No more pieces needed
		}

		// Use round-robin peer selection
		peerAddr := availablePeers[peerIndex%len(availablePeers)]
		peerIndex++

		// Mark as requested
		err := fd.MarkPieceAsRequested(pieceIndex, peerAddr)
		if err != nil {
			fmt.Printf("Failed to mark piece %d as requested: %v\n", pieceIndex, err)
			continue
		}

		// Create piece request
		pieceRequest := PieceRequest{
			PieceIndex: pieceIndex,
			PeerAddr:   peerAddr,
			Begin:      0,
			Length:     int(fd.metaInfo.Info.ChunkSize),
		}

		// Handle last piece which might be smaller
		if pieceIndex == len(fd.pieces)-1 {
			lastPieceSize := fd.metaInfo.Info.Size % fd.metaInfo.Info.ChunkSize
			if lastPieceSize != 0 {
				pieceRequest.Length = int(lastPieceSize)
			}
		}

		// Send to work queue
		select {
		case fd.WorkQueue <- pieceRequest:
			fmt.Printf("Requested piece %d from peer %s\n", pieceIndex, peerAddr)
		default:
			// Work queue is full, mark piece as needed again
			fd.mu.Lock()
			fd.pieces[pieceIndex].Status = Needed
			fd.pieces[pieceIndex].RequestedAt = time.Time{}
			fd.pieces[pieceIndex].RequestedFrom = ""
			fd.mu.Unlock()
			return // Exit the function if work queue is full
		}
	}
}

// GenerateBitfield creates a bitfield representing which pieces we have
// Each bit represents a piece: 1 = have, 0 = don't have
func (fd *FileDownloader) GenerateBitfield() []byte {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	// Calculate number of bytes needed
	numBytes := (len(fd.pieces) + 7) / 8
	bitfield := make([]byte, numBytes)

	for i, piece := range fd.pieces {
		if piece.Status == Have {
			byteIndex := i / 8
			bitIndex := i % 8
			bitfield[byteIndex] |= (1 << (7 - bitIndex))
		}
	}

	return bitfield
}

// PrintStatus prints the current download status for debugging
func (fd *FileDownloader) PrintStatus() {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	fmt.Printf("\n=== Download Status ===\n")
	fmt.Printf("File: %s (%d bytes)\n", fd.metaInfo.Info.Name, fd.metaInfo.Info.Size)
	fmt.Printf("Progress: %d/%d pieces (%.1f%%)\n",
		fd.completedPieces, fd.totalPieces,
		float64(fd.completedPieces)/float64(fd.totalPieces)*100)

	// Count pieces by status
	needed, requested, have := 0, 0, 0
	for _, piece := range fd.pieces {
		switch piece.Status {
		case Needed:
			needed++
		case Requested:
			requested++
		case Have:
			have++
		}
	}

	fmt.Printf("Pieces - Needed: %d, Requested: %d, Have: %d\n", needed, requested, have)
	fmt.Printf("=======================\n\n")
}
