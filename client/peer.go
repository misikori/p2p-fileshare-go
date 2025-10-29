package main

import (
	"log"
	"net"
	"os"
	"p2p-fileshare/metafile"
)

type Peer struct {
	conn         net.Conn
	Bitfield     Bitfield
	meta         metafile.FileInfo
	workQueue    chan *Request
	resultsQueue chan *Piece
}

func NewPeer(conn net.Conn, theirBitfield Bitfield, meta metafile.FileInfo, workQueue chan *Request, resultsQueue chan *Piece) *Peer {
	return &Peer{
		conn:         conn,
		Bitfield:     theirBitfield,
		meta:         meta,
		workQueue:    workQueue,
		resultsQueue: resultsQueue,
	}
}

func (p *Peer) handleRequest(req *Request) error {
	log.Printf("[%s] Received request for piece %d, offset %d, length %d", p.conn.RemoteAddr(), req.Index, req.Begin, req.Length)

	filePath := "shared/" + p.meta.Name
	file, err := os.Open(filePath)
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

	// Start goroutine to handle outgoing requests from workQueue
	go p.sendRequests()

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
		case MsgPiece:
			piece, err := ParsePiece(msg.Payload)
			if err != nil {
				log.Printf("[%s] Failed to parse piece: %v", p.conn.RemoteAddr(), err)
				continue
			}
			log.Printf("[%s] Received piece %d, offset %d, size %d", p.conn.RemoteAddr(), piece.Index, piece.Begin, len(piece.Data))
			// Send the piece to the downloader
			if p.resultsQueue != nil {
				p.resultsQueue <- piece
			}
		default:
			log.Printf("[%s] Received unknown message type: %d", p.conn.RemoteAddr(), msg.Type)
		}
	}
}

// sendRequests reads from the workQueue and sends Request messages to the peer
func (p *Peer) sendRequests() {
	if p.workQueue == nil {
		return
	}

	for req := range p.workQueue {
		log.Printf("[%s] Sending request for piece %d, offset %d, length %d", p.conn.RemoteAddr(), req.Index, req.Begin, req.Length)
		if err := req.Send(p.conn); err != nil {
			log.Printf("[%s] Failed to send request: %v", p.conn.RemoteAddr(), err)
			return
		}
	}
}
