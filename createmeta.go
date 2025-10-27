package main

import (
	"encoding/json"
	"log"
	"os"
	"p2p-fileshare/metafile"
)

func main() {
	meta, err := metafile.Create("http://tracker:8080", "./shared/dummy_file.txt")
	if err != nil {
		log.Fatalf("Error creating meta file: %v", err)
	}

	f, err := os.Create("./shared/dummy_file.txt.meta")
	if err != nil {
		log.Fatalf("Error creating meta file: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(meta)
	log.Println("Created ./shared/dummy_file.txt.meta")
}
