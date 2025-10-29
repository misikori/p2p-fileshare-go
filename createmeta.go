package main

import (
	"encoding/json"
	"log"
	"os"
	"p2p-fileshare/metafile"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run createmeta.go <file_path>\nExample: go run createmeta.go ./shared/myfile.txt")
	}

	trackerURL := "http://localhost:8080"
	filePath := os.Args[1]

	// Create metafile
	meta, err := metafile.Create(trackerURL, filePath)
	if err != nil {
		log.Fatalf("Error creating meta file: %v", err)
	}

	// Create .meta file with same name as original file
	metaFilePath := filePath + ".meta"
	f, err := os.Create(metaFilePath)
	if err != nil {
		log.Fatalf("Error creating meta file: %v", err)
	}
	defer f.Close()

	// Write metafile
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(meta); err != nil {
		log.Fatalf("Error encoding meta file: %v", err)
	}

	log.Printf("✓ Created %s", metaFilePath)
	log.Printf("  File: %s", meta.Info.Name)
	log.Printf("  Size: %d bytes", meta.Info.Size)
	log.Printf("  Pieces: %d", len(meta.Info.Hashes))
	log.Printf("  Chunk Size: %d bytes", meta.Info.ChunkSize)
	log.Printf("  Tracker: %s", trackerURL)
}
