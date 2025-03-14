package wisp

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
)

type WispConfig struct {
	DefaultBufferRemaining uint32
}

func CreateWispHandler(config WispConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		wispConnection := &wispConn{
			wsConn:  conn,
			streams: make(map[uint32]*wispStream),
			config:  config,
		}
		defer wispConnection.endAllWispStreams()

		wispConnection.sendContinuePacket(0, config.DefaultBufferRemaining)

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if len(message) < 5 {
				return
			}

			packetType, streamId, payload := parseWispPacket(message)

			if err := wispConnection.handlePacket(packetType, streamId, payload); err != nil {
				return
			}
		}
	}
}

type wispConn struct {
	wsConn  *websocket.Conn
	wsMutex sync.Mutex

	streams      map[uint32]*wispStream
	streamsMutex sync.RWMutex

	config WispConfig
}

type wispStream struct {
	streamId        uint32
	streamType      uint8
	conn            net.Conn
	bufferRemaining uint32
}

func (c *wispConn) handlePacket(packetType uint8, streamId uint32, payload []byte) error {
	switch packetType {
	case packetTypeConnect:
		return c.handleConnectPacket(streamId, payload)
	case packetTypeData:
		return c.handleDataPacket(streamId, payload)
	case packetTypeClose:
		return c.handleClosePacket(streamId, payload)
	default:
		return fmt.Errorf("handlePacket: unknown packet type %d for stream %d", packetType, streamId)
	}
}

func (c *wispConn) handleConnectPacket(streamId uint32, payload []byte) error {
	if len(payload) < 3 {
		return c.sendClosePacket(streamId, closeReasonInvalidInfo)
	}

	streamType := payload[0]
	port := strconv.FormatUint(uint64(binary.LittleEndian.Uint16(payload[1:3])), 10)
	hostname := string(payload[3:])

	var netConn net.Conn
	var err error

	switch streamType {
	case streamTypeTCP:
		netConn, err = net.Dial("tcp", net.JoinHostPort(hostname, port))
	case streamTypeUDP:
		netConn, err = net.Dial("udp", net.JoinHostPort(hostname, port))
	default:
		return c.sendClosePacket(streamId, closeReasonInvalidInfo)
	}

	if err != nil {
		// todo: identify error type and send appropriate close packet
		return c.sendClosePacket(streamId, closeReasonUnspecified)
	}

	wispDataStream := &wispStream{
		streamId:        streamId,
		streamType:      streamType,
		conn:            netConn,
		bufferRemaining: c.config.DefaultBufferRemaining,
	}

	c.streamsMutex.Lock()
	c.streams[streamId] = wispDataStream
	c.streamsMutex.Unlock()

	go c.readFromNetConn(wispDataStream)

	return nil
}

func (c *wispConn) handleDataPacket(streamId uint32, payload []byte) error {
	c.streamsMutex.RLock()
	stream, exists := c.streams[streamId]
	c.streamsMutex.RUnlock()

	if !exists {
		return c.sendClosePacket(streamId, closeReasonInvalidInfo)
	}

	_, err := stream.conn.Write(payload)
	if err != nil {
		// todo: identify error type and send appropriate close packet
		c.streamsMutex.Lock()
		c.endWispStream(streamId)
		c.streamsMutex.Unlock()

		return c.sendClosePacket(streamId, closeReasonUnspecified)
	}

	if stream.streamType == streamTypeTCP {
		if stream.bufferRemaining == 0 {
			stream.bufferRemaining = c.config.DefaultBufferRemaining
			return c.sendContinuePacket(streamId, c.config.DefaultBufferRemaining)
		} else {
			stream.bufferRemaining--
		}
	}
	return nil
}

func (c *wispConn) handleClosePacket(streamId uint32, payload []byte) error {
	if len(payload) < 1 {
		return nil
	}
	closeReason := payload[0]
	_ = closeReason // todo: handle close reason

	c.streamsMutex.Lock()
	c.endWispStream(streamId)
	c.streamsMutex.Unlock()
	return nil
}

func (c *wispConn) sendPacket(packet []byte) error {
	c.wsMutex.Lock()
	defer c.wsMutex.Unlock()
	return c.wsConn.WriteMessage(websocket.BinaryMessage, packet)
}

func (c *wispConn) sendClosePacket(streamId uint32, reason uint8) error {
	packet := createWispPacket(packetTypeClose, streamId, []byte{reason})
	return c.sendPacket(packet)
}

func (c *wispConn) sendDataPacket(streamId uint32, payload []byte) error {
	packet := createWispPacket(packetTypeData, streamId, payload)
	return c.sendPacket(packet)
}

func (c *wispConn) sendContinuePacket(streamId uint32, bufferRemaining uint32) error {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, bufferRemaining)
	packet := createWispPacket(packetTypeContinue, streamId, payload)
	return c.sendPacket(packet)
}

func (c *wispConn) endWispStream(streamId uint32) error {
	stream, exists := c.streams[streamId]
	if exists {
		delete(c.streams, streamId)
		return stream.conn.Close()
	}
	return nil
}

func (c *wispConn) endAllWispStreams() {
	c.streamsMutex.Lock()
	for streamId := range c.streams {
		c.endWispStream(streamId)
	}
	c.streamsMutex.Unlock()
}

func (c *wispConn) readFromNetConn(wispDataStream *wispStream) {
	buffer := make([]byte, 4096)
	for {
		n, err := wispDataStream.conn.Read(buffer)
		if err != nil {
			// todo: identify error and send appropriate close packet
			c.streamsMutex.Lock()
			c.endWispStream(wispDataStream.streamId)
			c.streamsMutex.Unlock()

			c.sendClosePacket(wispDataStream.streamId, closeReasonUnspecified)
			break
		}

		if err := c.sendDataPacket(wispDataStream.streamId, buffer[:n]); err != nil {
			c.streamsMutex.Lock()
			c.endWispStream(wispDataStream.streamId)
			c.streamsMutex.Unlock()

			c.sendClosePacket(wispDataStream.streamId, closeReasonVoluntary)
			break
		}
	}
}
