package main

import (
	"log"
	"net"
)

type Peer struct {
	conn     net.Conn
	Bitfield Bitfield
}

func NewPeer(conn net.Conn, theirBitfield Bitfield) *Peer {
	return &Peer{
		conn:     conn,
		Bitfield: theirBitfield,
	}
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
			log.Printf("[%s] Received REQUEST", p.conn.RemoteAddr())
		default:
			log.Printf("[%s] Received unknown message type: %d", p.conn.RemoteAddr(), msg.Type)
		}
	}
}
