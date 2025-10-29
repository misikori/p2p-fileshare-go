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
