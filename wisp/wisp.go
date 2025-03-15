package wisp

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

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

			wispConnection.handlePacket(message)
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

	dataPackets      [][]byte
	dataPacketsMutex sync.Mutex

	ready           atomic.Bool
	connEstablished chan bool
	isSendingData   atomic.Bool
}

func (c *wispConn) handlePacket(message []byte) {
	if len(message) < 5 {
		return
	}
	packetType, streamId, payload := parseWispPacket(message)

	switch packetType {
	case packetTypeConnect:
		stream := &wispStream{
			streamId:        streamId,
			connEstablished: make(chan bool),
		}

		c.streamsMutex.Lock()
		c.streams[streamId] = stream
		c.streamsMutex.Unlock()

		go c.handleConnectPacket(stream, payload)
	case packetTypeData:
		c.streamsMutex.RLock()
		stream, exists := c.streams[streamId]
		c.streamsMutex.RUnlock()
		if exists {
			stream.dataPacketsMutex.Lock()
			stream.dataPackets = append(stream.dataPackets, payload)
			stream.dataPacketsMutex.Unlock()

			if !stream.isSendingData.Load() {
				go c.handleDataPacket(stream)
			}
		}
	case packetTypeClose:
		go c.handleClosePacket(streamId, payload)
	default:
		return
	}
}

func (c *wispConn) handleConnectPacket(stream *wispStream, payload []byte) {
	if len(payload) < 3 {
		return
	}
	streamType := payload[0]
	port := strconv.FormatUint(uint64(binary.LittleEndian.Uint16(payload[1:3])), 10)
	hostname := string(payload[3:])

	stream.streamType = streamType
	stream.bufferRemaining = c.config.DefaultBufferRemaining

	var err error
	switch streamType {
	case streamTypeTCP:
		stream.conn, err = net.Dial("tcp", net.JoinHostPort(hostname, port))
	case streamTypeUDP:
		stream.conn, err = net.Dial("udp", net.JoinHostPort(hostname, port))
	default:
		return
	}

	if err != nil {
		stream.connEstablished <- false

		c.streamsMutex.Lock()
		c.endWispStream(stream.streamId)
		c.streamsMutex.Unlock()

		if err := c.sendClosePacket(stream.streamId, closeReasonNetworkError); err != nil {
			c.wsConn.Close()
		}
		return
	}

	stream.connEstablished <- true
	stream.ready.Store(true)

	var closeReason uint8
	if err := stream.readFromNetConn(c); err == io.EOF {
		closeReason = closeReasonVoluntary
	} else {
		closeReason = closeReasonNetworkError
	}

	c.streamsMutex.Lock()
	c.endWispStream(stream.streamId)
	c.streamsMutex.Unlock()

	if err := c.sendClosePacket(stream.streamId, closeReason); err != nil {
		c.wsConn.Close()
	}
}

func (c *wispConn) handleDataPacket(stream *wispStream) {
	stream.isSendingData.Store(true)
	defer stream.isSendingData.Store(false)

	if !stream.ready.Load() {
		if !<-stream.connEstablished {
			return
		}
	}

	for {
		stream.dataPacketsMutex.Lock()
		dataPackets := stream.dataPackets
		stream.dataPackets = stream.dataPackets[len(stream.dataPackets):]
		stream.dataPacketsMutex.Unlock()
		if len(dataPackets) == 0 {
			break
		}

		for _, packet := range dataPackets {
			_, err := stream.conn.Write(packet)
			if err != nil {
				c.streamsMutex.Lock()
				c.endWispStream(stream.streamId)
				c.streamsMutex.Unlock()

				if err := c.sendClosePacket(stream.streamId, closeReasonNetworkError); err != nil {
					c.wsConn.Close()
				}
			}

			if stream.streamType == streamTypeTCP {
				if stream.bufferRemaining == 0 {
					stream.bufferRemaining = c.config.DefaultBufferRemaining
					c.sendContinuePacket(stream.streamId, c.config.DefaultBufferRemaining)
				} else {
					stream.bufferRemaining--
				}
			}
		}
	}
}

func (c *wispConn) handleClosePacket(streamId uint32, payload []byte) error {
	if len(payload) < 1 {
		return nil
	}
	closeReason := payload[0]
	_ = closeReason

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

func (c *wispConn) endWispStream(streamId uint32) {
	stream, exists := c.streams[streamId]
	if exists {
		if stream.ready.Load() {
			stream.conn.Close()
		}
		delete(c.streams, streamId)
	}
}

func (c *wispConn) endAllWispStreams() {
	c.streamsMutex.Lock()
	for streamId := range c.streams {
		c.endWispStream(streamId)
	}
	c.streamsMutex.Unlock()
}

func (s *wispStream) readFromNetConn(wispConnection *wispConn) error {
	buffer := make([]byte, 4096)
	for {
		n, err := s.conn.Read(buffer)
		if err != nil {
			return err
		}

		if err := wispConnection.sendDataPacket(s.streamId, buffer[:n]); err != nil {
			return err
		}
	}
}
