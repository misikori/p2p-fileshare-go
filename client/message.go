package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

type MessageType uint8

const (
	MsgBitfield MessageType = 5
	MsgRequest  MessageType = 6
	MsgPiece    MessageType = 7
)

type Message struct {
	Type    MessageType
	Payload []byte
}

// defines the payload for a Req message. Asks for a block of data from a Piece.
type Request struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

// defines the payload for a Piece message. It sends a block of data.
type Piece struct {
	Index uint32
	Begin uint32
	Data  []byte
}

func ParseRequest(payload []byte) (*Request, error) {
	if len(payload) != 12 {
		return nil, fmt.Errorf("invalid Request payload length: %d", len(payload))
	}
	return &Request{
		Index:  binary.BigEndian.Uint32(payload[0:4]),
		Begin:  binary.BigEndian.Uint32(payload[4:8]),
		Length: binary.BigEndian.Uint32(payload[8:12]),
	}, nil
}

func ParsePiece(payload []byte) (*Piece, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("invalid Piece payload length: %d", len(payload))
	}
	return &Piece{
		Index: binary.BigEndian.Uint32(payload[0:4]),
		Begin: binary.BigEndian.Uint32(payload[4:8]),
		Data:  payload[8:],
	}, nil
}

func (r *Request) Send(conn net.Conn) error {
	msg := Message{
		Type:    MsgRequest,
		Payload: make([]byte, 12),
	}

	binary.BigEndian.PutUint32(msg.Payload[0:4], r.Index)
	binary.BigEndian.PutUint32(msg.Payload[4:8], r.Begin)
	binary.BigEndian.PutUint32(msg.Payload[8:12], r.Length)

	return msg.Send(conn)
}

func (p *Piece) Send(conn net.Conn) error {
	payloadLen := 4 + 4 + len(p.Data)
	msg := Message{
		Type:    MsgPiece,
		Payload: make([]byte, payloadLen),
	}

	binary.BigEndian.PutUint32(msg.Payload[0:4], p.Index)
	binary.BigEndian.PutUint32(msg.Payload[4:8], p.Begin)
	copy(msg.Payload[8:], p.Data)

	return msg.Send(conn)
}

func (m *Message) Send(conn net.Conn) error {
	length := uint32(1 + len(m.Payload))

	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = byte(m.Type)
	copy(buf[5:], m.Payload)

	_, err := conn.Write(buf)
	return err
}

func ReadMessage(conn net.Conn) (*Message, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf)
	if length == 0 {
		return nil, fmt.Errorf("empty message")
	}

	msgBuf := make([]byte, length)
	if _, err := io.ReadFull(conn, msgBuf); err != nil {
		return nil, err
	}

	return &Message{
		Type:    MessageType(msgBuf[0]),
		Payload: msgBuf[1:],
	}, nil
}
