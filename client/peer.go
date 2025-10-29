package main

import (
	"log"
	"net"
	"os"
	"p2p-fileshare/metafile"
)

type Peer struct {
	conn     net.Conn
	Bitfield Bitfield
	meta     metafile.FileInfo
}

func NewPeer(conn net.Conn, theirBitfield Bitfield, meta metafile.FileInfo) *Peer {
	return &Peer{
		conn:     conn,
		Bitfield: theirBitfield,
		meta:     meta,
	}
}

func (p *Peer) handleRequest(req *Request) error {
	log.Printf("[%s] Received request for piece %d, offset %d, length %d", p.conn.RemoteAddr(), req.Index, req.Begin, req.Length)

	file, err := os.Open("./shared/" + p.meta.Name)
	if err != nil {
		log.Printf("Failed to open file: %v", err)
		return err
	}
	defer file.Close()

	// seek to the appropriate position.
	pieceSize := p.meta.ChunkSize
	offset := int64(req.Index)*pieceSize + int64(req.Begin)

	buf := make([]byte, req.Length)
	if _, err := file.ReadAt(buf, offset); err != nil {
		log.Printf("Failed to read from file: %v", err)
		return err
	}

	pieceMsg := &Piece{
		Index: req.Index,
		Begin: req.Begin,
		Data:  buf,
	}
	return pieceMsg.Send(p.conn)
}

func (p *Peer) RunMessageLoop() error {
	defer p.conn.Close()
	log.Printf("[%s] Starting message loop", p.conn.RemoteAddr())

	for {
		msg, err := ReadMessage(p.conn)
		if err != nil {
			return err
		}

		switch msg.Type {
		case MsgRequest:
			req, err := ParseRequest(msg.Payload)
			if err != nil {
				log.Printf("Failed to parse request: %v", err)
				continue
			}
			if err := p.handleRequest(req); err != nil {
				log.Printf("Failed to handle request: %v", err)
				continue
			}
		default:
			log.Printf("[%s] Received unknown message type: %d", p.conn.RemoteAddr(), msg.Type)
		}
	}
}
