package wisp

import (
	"encoding/binary"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
)

type Config struct {
	BufferRemainingLength uint32
	Blacklist             struct {
		Hostnames map[string]struct{}
	}
	DisableUDP    bool
	TcpBufferSize uint
}

func CreateWispHandler(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer wsConn.Close()

		wispConnection := &wispConnection{
			wsConn:  wsConn,
			streams: make(map[uint32]*wispStream),
			config:  config,
		}
		defer wispConnection.deleteAllWispStreams()

		wispConnection.sendContinuePacket(0, config.BufferRemainingLength)

		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				return
			}

			wispConnection.handlePacket(message)
		}
	}
}

type wispConnection struct {
	wsConn  *websocket.Conn
	wsMutex sync.Mutex

	streams      map[uint32]*wispStream
	streamsMutex sync.RWMutex

	config Config
}

func (c *wispConnection) handlePacket(packet []byte) {
	if len(packet) < 5 {
		return
	}
	packetType, streamId, payload := parseWispPacket(packet)

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
	}

	c.streamsMutex.Lock()
	c.streams[streamId] = stream
	c.streamsMutex.Unlock()

	go stream.handleConnect(streamType, port, hostname)
}

func (c *wispConnection) handleDataPacket(streamId uint32, payload []byte) {
	c.streamsMutex.RLock()
	stream, exists := c.streams[streamId]
	c.streamsMutex.RUnlock()
	if !exists {
		c.sendClosePacket(streamId, closeReasonInvalidInfo)
		return
	}
	stream.dataQueueMutex.Lock()
	stream.dataQueue = append(stream.dataQueue, payload)
	stream.dataQueueMutex.Unlock()

	go stream.handleData()
}

func (c *wispConnection) handleClosePacket(streamId uint32, payload []byte) {
	c.streamsMutex.RLock()
	stream, exists := c.streams[streamId]
	c.streamsMutex.RUnlock()
	if !exists {
		return
	}

	if len(payload) < 1 {
		return
	}
	closeReason := payload[0]

	go stream.handleClose(closeReason)
}

func (c *wispConnection) sendPacket(packet []byte) {
	c.wsMutex.Lock()
	defer c.wsMutex.Unlock()
	if err := c.wsConn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		c.wsConn.Close()
	}
}

func (c *wispConnection) sendDataPacket(streamId uint32, data []byte) {
	packet := createWispPacket(packetTypeData, streamId, data)
	c.sendPacket(packet)
}

func (c *wispConnection) sendContinuePacket(streamId uint32, bufferRemaining uint32) {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, bufferRemaining)
	packet := createWispPacket(packetTypeContinue, streamId, payload)
	c.sendPacket(packet)
}

func (c *wispConnection) sendClosePacket(streamId uint32, reason uint8) {
	packet := createWispPacket(packetTypeClose, streamId, []byte{reason})
	c.sendPacket(packet)
}

func (c *wispConnection) deleteAllWispStreams() {
	c.streamsMutex.Lock()
	for streamId, stream := range c.streams {
		stream.closeConnection()
		delete(c.streams, streamId)
	}
	c.streamsMutex.Unlock()
}
