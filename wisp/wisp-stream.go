package wisp

import (
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
)

type wispStream struct {
	wispConn *wispConnection

	streamId        uint32
	streamType      uint8
	conn            net.Conn
	bufferRemaining uint32

	dataQueue chan []byte

	ready           atomic.Bool
	connEstablished chan bool

	sendDataOnce sync.Once
	closeOnce    sync.Once

	isOpen atomic.Bool
}

func (s *wispStream) handleConnect(streamType uint8, port string, hostname string) {
	if _, blacklisted := s.wispConn.config.Blacklist.Hostnames[hostname]; blacklisted {
		s.close(closeReasonBlocked)
		return
	}

	s.streamType = streamType
	s.bufferRemaining = s.wispConn.config.BufferRemainingLength

	var err error
	switch streamType {
	case streamTypeTCP:
		s.conn, err = net.Dial("tcp", net.JoinHostPort(hostname, port))
	case streamTypeUDP:
		if s.wispConn.config.DisableUDP {
			s.close(closeReasonBlocked)
			return
		}
		s.conn, err = net.Dial("udp", net.JoinHostPort(hostname, port))
	default:
		s.close(closeReasonInvalidInfo)
		return
	}

	if err != nil {
		s.close(closeReasonNetworkError)
		return
	}

	if s.streamType == streamTypeTCP {
		tcpConn := s.conn.(*net.TCPConn)
		tcpConn.SetNoDelay(s.wispConn.config.TcpNoDelay)
	}

	s.connEstablished <- true
	s.ready.Store(true)

	go s.readFromConnection()
}

func (s *wispStream) handleData() {
	if !<-s.connEstablished {
		return
	}

	for data := range s.dataQueue {
		_, err := s.conn.Write(data)
		if err != nil {
			s.close(closeReasonNetworkError)
			return
		}

		if s.streamType == streamTypeTCP {
			s.bufferRemaining--
			if s.bufferRemaining == 0 {
				s.bufferRemaining = s.wispConn.config.BufferRemainingLength
				s.sendContinue(s.bufferRemaining)
			}
		}
	}
}

func (s *wispStream) handleClose(reason uint8) {
	_ = reason
	s.close(closeReasonVoluntary)
}

func (s *wispStream) sendData(payload []byte) {
	s.wispConn.sendDataPacket(s.streamId, payload)
}

func (s *wispStream) sendContinue(bufferRemaining uint32) {
	s.wispConn.sendContinuePacket(s.streamId, bufferRemaining)
}

func (s *wispStream) sendClose(reason uint8) {
	s.wispConn.sendClosePacket(s.streamId, reason)
}

func (s *wispStream) closeConnection() {
	if s.ready.Load() {
		s.conn.Close()
	}
}

func (s *wispStream) readFromConnection() {
	var closeReason uint8

	buffer := make([]byte, s.wispConn.config.TcpBufferSize)

	prevSent := make(chan struct{}, 1)
	prevSent <- struct{}{}
	for {
		n, err := s.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				closeReason = closeReasonVoluntary
			} else {
				closeReason = closeReasonNetworkError
			}
			break
		}

		data := slices.Clone(buffer[:n])

		<-prevSent
		go func() {
			s.sendData(data)
			prevSent <- struct{}{}
		}()
	}

	s.close(closeReason)
}

func (s *wispStream) close(reason uint8) {
	s.closeOnce.Do(func() {
		s.wispConn.streams.Delete(s.streamId)

		s.closeConnection()

		s.isOpen.Store(false)
		close(s.dataQueue)

		select {
		case s.connEstablished <- false:
		default:
		}
		close(s.connEstablished)

		s.sendClose(reason)
	})
}
