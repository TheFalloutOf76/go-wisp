package wisp

import (
	"encoding/binary"
	"strconv"
	"sync"

	"github.com/lxzan/gws"
)

type wispConnection struct {
	wsConn *gws.Conn

	streams sync.Map

	config *Config
}

func (c *wispConnection) handlePacket(packetType uint8, streamId uint32, payload []byte) {
	switch packetType {
	case packetTypeConnect:
		c.handleConnectPacket(streamId, payload)
	case packetTypeData:
		c.handleDataPacket(streamId, payload)
	case packetTypeClose:
		c.handleClosePacket(streamId, payload)
	default:
		return
	}
}

func (c *wispConnection) handleConnectPacket(streamId uint32, payload []byte) {
	if len(payload) < 3 {
		return
	}
	streamType := payload[0]
	port := strconv.FormatUint(uint64(binary.LittleEndian.Uint16(payload[1:3])), 10)
	hostname := string(payload[3:])

	stream := &wispStream{
		wispConn:        c,
		streamId:        streamId,
		connEstablished: make(chan bool, 1),
		dataQueue:       make(chan []byte, c.config.BufferRemainingLength),
	}
	stream.isOpen.Store(true)

	c.streams.Store(streamId, stream)

	go stream.handleConnect(streamType, port, hostname)
}

func (c *wispConnection) handleDataPacket(streamId uint32, payload []byte) {
	streamAny, exists := c.streams.Load(streamId)
	if !exists {
		c.sendClosePacket(streamId, closeReasonInvalidInfo)
		return
	}
	stream := streamAny.(*wispStream)

	if !stream.isOpen.Load() {
		return
	}

	select {
	case stream.dataQueue <- payload:
	default:
	}

	go stream.sendDataOnce.Do(stream.handleData)
}

func (c *wispConnection) handleClosePacket(streamId uint32, payload []byte) {
	if len(payload) < 1 {
		return
	}
	closeReason := payload[0]

	streamAny, exists := c.streams.Load(streamId)
	if !exists {
		return
	}
	stream := streamAny.(*wispStream)

	go stream.handleClose(closeReason)
}

func (c *wispConnection) sendPacket(packetType uint8, streamId uint32, payload []byte) {
	packet := createWispPacket(packetType, streamId, payload)
	if err := c.wsConn.WriteMessage(gws.OpcodeBinary, packet); err != nil {
		c.wsConn.NetConn().Close()
	}
}

func (c *wispConnection) sendDataPacket(streamId uint32, data []byte) {
	c.sendPacket(packetTypeData, streamId, data)
}

func (c *wispConnection) sendContinuePacket(streamId uint32, bufferRemaining uint32) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, bufferRemaining)
	c.sendPacket(packetTypeContinue, streamId, payload)
}

func (c *wispConnection) sendClosePacket(streamId uint32, reason uint8) {
	payload := []byte{reason}
	c.sendPacket(packetTypeClose, streamId, payload)
}

func (c *wispConnection) deleteAllWispStreams() {
	c.streams.Range(func(key, value any) bool {
		stream, ok := value.(*wispStream)
		if !ok {
			return true
		}
		stream.close(closeReasonUnspecified)
		return true
	})
}
