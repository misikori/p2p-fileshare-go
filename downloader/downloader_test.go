package downloader

import (
	"bytes"
	"os"
	"testing"
	"time"

	"p2p-fileshare/metafile"
)

func TestDownloaderBasicFlow(t *testing.T) {
	// Create test content
	testContent := []byte("Hello, World! This is test content for our P2P downloader system. " +
		"It needs to be long enough to create multiple chunks for proper testing. " +
		"This will help us verify that the piece-based download system works correctly.")
	
	testFile := "test_input.txt"
	outputFile := "test_output.txt"
	
	// Create test file
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	// Create metafile with small chunk size for testing
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	t.Logf("Created metafile with %d pieces, chunk size: %d", len(meta.Info.Hashes), meta.Info.ChunkSize)
	
	// Create downloader
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Verify initial state
	if downloader.IsComplete() {
		t.Error("Downloader should not be complete initially")
	}
	
	completed, total, percentage := downloader.GetProgress()
	if completed != 0 || total != len(meta.Info.Hashes) || percentage != 0 {
		t.Errorf("Initial progress should be 0/total/0%%, got %d/%d/%.1f%%", completed, total, percentage)
	}
	
	// Start processing pieces in background
	go downloader.ProcessPieceData()
	
	// Simulate downloading each piece
	originalFile, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open original file: %v", err)
	}
	defer originalFile.Close()
	
	for i := 0; i < len(meta.Info.Hashes); i++ {
		// Read the actual piece data from the original file
		pieceData := make([]byte, meta.Info.ChunkSize)
		originalFile.Seek(int64(i)*meta.Info.ChunkSize, 0)
		n, _ := originalFile.Read(pieceData)
		
		// Handle last piece which might be smaller
		if n < int(meta.Info.ChunkSize) {
			pieceData = pieceData[:n]
		}
		
		// Send to results queue
		downloader.ResultsQueue <- PieceData{
			PieceIndex: i,
			Begin:      0,
			Data:       pieceData,
			PeerAddr:   "mock-peer",
		}
		
		// Give it time to process
		time.Sleep(10 * time.Millisecond)
		
		// Verify progress
		completed, total, percentage = downloader.GetProgress()
		expectedCompleted := i + 1
		expectedPercentage := float64(expectedCompleted) / float64(total) * 100
		
		if completed != expectedCompleted {
			t.Errorf("After piece %d: expected %d completed, got %d", i, expectedCompleted, completed)
		}
		if percentage < expectedPercentage-0.1 || percentage > expectedPercentage+0.1 {
			t.Errorf("After piece %d: expected %.1f%% progress, got %.1f%%", i, expectedPercentage, percentage)
		}
	}
	
	// Wait for completion
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeout:
			t.Fatal("Download did not complete within timeout")
		case <-ticker.C:
			if downloader.IsComplete() {
				goto completed
			}
		}
	}
	
completed:
	// Verify the downloaded file matches original
	downloadedContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	
	if !bytes.Equal(downloadedContent, testContent) {
		t.Errorf("Downloaded content doesn't match original.\nExpected: %s\nGot: %s", 
			string(testContent), string(downloadedContent))
	}
	
	// Verify final state
	if !downloader.IsComplete() {
		t.Error("Downloader should be complete")
	}
	
	completed, total, percentage = downloader.GetProgress()
	if completed != total || percentage != 100.0 {
		t.Errorf("Final progress should be %d/%d/100%%, got %d/%d/%.1f%%", total, total, completed, total, percentage)
	}
}

func TestPieceVerification(t *testing.T) {
	// Create test content
	testContent := []byte("Test content for piece verification")
	testFile := "test_verify.txt"
	outputFile := "test_verify_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	// Create metafile
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	// Create downloader
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Test with correct data
	correctData := PieceData{
		PieceIndex: 0,
		Begin:      0,
		Data:       testContent,
		PeerAddr:   "test-peer",
	}
	
	if !downloader.VerifyPiece(correctData) {
		t.Error("Verification should pass for correct data")
	}
	
	// Test with incorrect data
	incorrectData := PieceData{
		PieceIndex: 0,
		Begin:      0,
		Data:       []byte("Wrong data"),
		PeerAddr:   "test-peer",
	}
	
	if downloader.VerifyPiece(incorrectData) {
		t.Error("Verification should fail for incorrect data")
	}
	
	// Test with invalid piece index
	invalidIndexData := PieceData{
		PieceIndex: 999,
		Begin:      0,
		Data:       testContent,
		PeerAddr:   "test-peer",
	}
	
	if downloader.VerifyPiece(invalidIndexData) {
		t.Error("Verification should fail for invalid piece index")
	}
}

func TestPieceStateManagement(t *testing.T) {
	// Create minimal test setup
	testContent := []byte("Test content")
	testFile := "test_state.txt"
	outputFile := "test_state_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Test initial state
	if downloader.GetPieceStatus(0) != Needed {
		t.Error("Initial piece status should be Needed")
	}
	
	// Test next piece to request
	nextPiece := downloader.GetNextPieceToRequest()
	if nextPiece != 0 {
		t.Errorf("Next piece to request should be 0, got %d", nextPiece)
	}
	
	// Test marking piece as requested
	err = downloader.MarkPieceAsRequested(0, "test-peer")
	if err != nil {
		t.Errorf("Failed to mark piece as requested: %v", err)
	}
	
	if downloader.GetPieceStatus(0) != Requested {
		t.Error("Piece status should be Requested after marking")
	}
	
	// Test that next piece to request is now -1 (none available)
	nextPiece = downloader.GetNextPieceToRequest()
	if nextPiece != -1 {
		t.Errorf("Next piece to request should be -1, got %d", nextPiece)
	}
	
	// Test error when marking already requested piece
	err = downloader.MarkPieceAsRequested(0, "another-peer")
	if err == nil {
		t.Error("Should get error when marking already requested piece")
	}
	
	// Test stale request reset
	time.Sleep(10 * time.Millisecond)
	resetPieces := downloader.ResetStaleRequests(5 * time.Millisecond)
	if len(resetPieces) != 1 || resetPieces[0] != 0 {
		t.Errorf("Should reset 1 stale piece (piece 0), got %v", resetPieces)
	}
	
	if downloader.GetPieceStatus(0) != Needed {
		t.Error("Piece status should be Needed after stale reset")
	}
}

func TestBitfieldGeneration(t *testing.T) {
	// Create test content that will generate multiple pieces
	testContent := make([]byte, int(metafile.ChunkSize)*2+100) // 2+ pieces
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}
	
	testFile := "test_bitfield.txt"
	outputFile := "test_bitfield_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Test initial bitfield (should be all zeros)
	bitfield := downloader.GenerateBitfield()
	for _, b := range bitfield {
		if b != 0 {
			t.Error("Initial bitfield should be all zeros")
			break
		}
	}
	
	// Manually mark some pieces as "Have"
	downloader.mu.Lock()
	downloader.pieces[0].Status = Have
	downloader.pieces[2].Status = Have
	downloader.completedPieces = 2
	downloader.mu.Unlock()
	
	// Test updated bitfield
	bitfield = downloader.GenerateBitfield()
	
	// Check that piece 0 is set (bit 7 of byte 0)
	if (bitfield[0] & (1 << 7)) == 0 {
		t.Error("Piece 0 should be set in bitfield")
	}
	
	// Check that piece 1 is not set (bit 6 of byte 0)
	if (bitfield[0] & (1 << 6)) != 0 {
		t.Error("Piece 1 should not be set in bitfield")
	}
	
	// Check that piece 2 is set (bit 5 of byte 0)
	if (bitfield[0] & (1 << 5)) == 0 {
		t.Error("Piece 2 should be set in bitfield")
	}
}

func TestAvailablePieces(t *testing.T) {
	// Create test setup with multiple pieces
	testContent := make([]byte, int(metafile.ChunkSize)*4) // 4 pieces
	testFile := "test_available.txt"
	outputFile := "test_available_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Create a peer bitfield: peer has pieces 0, 2, and 3
	// Bitfield: 10110000 (bits 7,5,4 set)
	peerBitfield := []byte{0xB0} // 10110000 in binary
	
	availablePieces := downloader.GetAvailablePieces(peerBitfield)
	
	// Should return pieces 0, 2, 3 since we need all pieces and peer has these
	expectedPieces := []int{0, 2, 3}
	if len(availablePieces) != len(expectedPieces) {
		t.Errorf("Expected %d available pieces, got %d", len(expectedPieces), len(availablePieces))
	}
	
	for i, expected := range expectedPieces {
		if i >= len(availablePieces) || availablePieces[i] != expected {
			t.Errorf("Expected piece %d at index %d, got %v", expected, i, availablePieces)
		}
	}
	
	// Mark piece 0 as "Have" and test again
	downloader.mu.Lock()
	downloader.pieces[0].Status = Have
	downloader.mu.Unlock()
	
	availablePieces = downloader.GetAvailablePieces(peerBitfield)
	expectedPieces = []int{2, 3} // Should not include piece 0 anymore
	
	if len(availablePieces) != len(expectedPieces) {
		t.Errorf("After marking piece 0 as Have: expected %d available pieces, got %d", 
			len(expectedPieces), len(availablePieces))
	}
}

func TestRequestNextPieces(t *testing.T) {
	// Create test setup
	testContent := make([]byte, int(metafile.ChunkSize)*3) // 3 pieces
	testFile := "test_request.txt"
	outputFile := "test_request_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	// Test with available peers
	availablePeers := []string{"peer1:8080", "peer2:8080", "peer3:8080"}
	maxConcurrentRequests := 2
	
	// Request pieces
	downloader.RequestNextPieces(availablePeers, maxConcurrentRequests)
	
	// Check that 2 pieces were requested
	requestedCount := 0
	for _, piece := range downloader.pieces {
		if piece.Status == Requested {
			requestedCount++
		}
	}
	
	if requestedCount != maxConcurrentRequests {
		t.Errorf("Expected %d pieces to be requested, got %d", maxConcurrentRequests, requestedCount)
	}
	
	// Check that requests were added to WorkQueue
	workQueueCount := len(downloader.WorkQueue)
	if workQueueCount != maxConcurrentRequests {
		t.Errorf("Expected %d requests in WorkQueue, got %d", maxConcurrentRequests, workQueueCount)
	}
	
	// Drain the work queue and verify requests
	for i := 0; i < workQueueCount; i++ {
		request := <-downloader.WorkQueue
		if request.PieceIndex < 0 || request.PieceIndex >= len(downloader.pieces) {
			t.Errorf("Invalid piece index in request: %d", request.PieceIndex)
		}
		if request.Length != int(meta.Info.ChunkSize) {
			t.Errorf("Expected request length %d, got %d", meta.Info.ChunkSize, request.Length)
		}
	}
	
	// Test with no available peers
	downloader.RequestNextPieces([]string{}, maxConcurrentRequests)
	
	// Should not add any new requests
	if len(downloader.WorkQueue) != 0 {
		t.Error("Should not add requests when no peers available")
	}
}

func TestFileInfo(t *testing.T) {
	testContent := []byte("Test file info")
	testFile := "test_info.txt"
	outputFile := "test_info_output.txt"
	
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)
	defer os.Remove(outputFile)
	
	meta, err := metafile.Create("test://tracker", testFile)
	if err != nil {
		t.Fatalf("Failed to create metafile: %v", err)
	}
	
	downloader, err := NewFileDownloader(meta, outputFile)
	if err != nil {
		t.Fatalf("Failed to create downloader: %v", err)
	}
	defer downloader.Close()
	
	name, size, chunkSize := downloader.GetFileInfo()
	
	if name != meta.Info.Name {
		t.Errorf("Expected name %s, got %s", meta.Info.Name, name)
	}
	if size != meta.Info.Size {
		t.Errorf("Expected size %d, got %d", meta.Info.Size, size)
	}
	if chunkSize != meta.Info.ChunkSize {
		t.Errorf("Expected chunk size %d, got %d", meta.Info.ChunkSize, chunkSize)
	}
}