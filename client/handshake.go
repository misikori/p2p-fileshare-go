package main

import (
	"fmt"
	"io"
	"log"
	"net"
)

const (
	ProtocolName = "OurP2PProtocol" // string to identify our protocol.
	ReservedLen  = 8                // 8 bytes for future flags
	InfoHashLen  = 32
	PeerIDLen    = 20
)

var (
	ProtocolNameLen = len(ProtocolName)
	HandshakeLen    = 1 + ProtocolNameLen + ReservedLen + InfoHashLen + PeerIDLen
)

type Handshake struct {
	Pstr     string
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
}

func (h *Handshake) Send(conn net.Conn) error {
	log.Printf("[%s] Sending handshake with infoHash: %x", conn.RemoteAddr(), h.InfoHash)
	buf := make([]byte, HandshakeLen)
	buf[0] = byte(ProtocolNameLen)
	copy(buf[1:], ProtocolName)
	// Fill reserved bytes with zeros (explicitly)
	copy(buf[1+ProtocolNameLen:1+ProtocolNameLen+ReservedLen], make([]byte, ReservedLen))

	copy(buf[1+ProtocolNameLen+ReservedLen:], h.InfoHash[:])
	copy(buf[1+ProtocolNameLen+ReservedLen+InfoHashLen:], h.PeerID[:])

	_, err := conn.Write(buf)

	return err
}

func (h *Handshake) Read(conn net.Conn) error {

	buf := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}

	pstrLen := int(buf[0])
	if pstrLen != ProtocolNameLen {
		return fmt.Errorf("invalid protocol length: %d", pstrLen)
	}

	h.Pstr = string(buf[1 : 1+pstrLen])
	if h.Pstr != ProtocolName {
		return fmt.Errorf("invalid protocol name: %s", h.Pstr)
	}

	copy(h.InfoHash[:], buf[1+pstrLen+ReservedLen:1+pstrLen+ReservedLen+InfoHashLen])
	copy(h.PeerID[:], buf[1+pstrLen+ReservedLen+InfoHashLen:])

	log.Printf("[%s] Received handshake with infoHash: %x", conn.RemoteAddr(), h.InfoHash)

	return nil
}
