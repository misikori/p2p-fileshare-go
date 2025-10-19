package metafile

import (
	"crypto/sha1"
	"encoding/json"
	"io"
	"os"
)

const ChunkSize = 256 * 1024

type FileInfo struct {
	Name      string   `json:"name"`
	Size      int64    `json:"size"`
	ChunkSize int64    `json:"chunk_size"`
	Hashes    [][]byte `json:"hashes"`
}

type MetaInfo struct {
	Info       FileInfo `json:"info"`
	InfoHash   []byte   `json:"info_hash"`
	TrackerURL string   `json:"tracker_url"`
}

func Create(trackerURL, filePath string) (*MetaInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stats, err := file.Stat()
	if err != nil {
		return nil, err
	}

	info := FileInfo{
		Name:      stats.Name(),
		Size:      stats.Size(),
		ChunkSize: ChunkSize,
		Hashes:    [][]byte{},
	}

	buffer := make([]byte, ChunkSize)
	for {
		bytesRead, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunk := buffer[:bytesRead]
		hash := sha1.Sum(chunk)
		info.Hashes = append(info.Hashes, hash[:])
	}

	infoBytes, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	infoHash := sha1.Sum(infoBytes)

	meta := &MetaInfo{
		Info:       info,
		InfoHash:   infoHash[:],
		TrackerURL: trackerURL,
	}
	return meta, nil
}

func Parse(filePath string) (*MetaInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var meta MetaInfo
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
