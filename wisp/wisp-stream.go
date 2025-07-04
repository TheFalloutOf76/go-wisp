package wisp

import (
	"context"
	"io"
	"net"
	"sync"

	"golang.org/x/net/proxy"
)

type wispStream struct {
	wispConn *wispConnection

	streamId        uint32
	streamType      uint8
	conn            net.Conn
	bufferRemaining uint32

	connEstablished chan bool
	dataQueue       chan []byte

	isOpen      bool
	isOpenMutex sync.RWMutex
}

func (s *wispStream) handleConnect(streamType uint8, port string, hostname string) {
	if _, blacklisted := s.wispConn.config.Blacklist.Hostnames[hostname]; blacklisted {
		s.close(closeReasonBlocked)
		return
	}

	var resolvedHostname string = hostname
	if _, whitelisted := s.wispConn.config.Whitelist.Hostnames[hostname]; !whitelisted && s.wispConn.config.DnsServer != "" {
		resolver := net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "udp", s.wispConn.config.DnsServer)
			},
		}

		ips, err := resolver.LookupIPAddr(context.Background(), hostname)
		if err != nil {
			s.close(closeReasonUnreachable)
			return
		}

		resolvedHostname = ips[0].IP.String()
		if resolvedHostname == "0.0.0.0" || resolvedHostname == "::" {
			s.close(closeReasonBlocked)
			return
		}
	}

	s.streamType = streamType
	s.bufferRemaining = s.wispConn.config.BufferRemainingLength

	destination := net.JoinHostPort(hostname, port)

	netDialer := net.Dial
	var err error
	switch streamType {
	case streamTypeTCP:
		if s.wispConn.config.Proxy != "" {
			dialer, proxyErr := proxy.SOCKS5("tcp", s.wispConn.config.Proxy, nil, proxy.Direct)
			if proxyErr != nil {
				s.close(closeReasonNetworkError)
				return
			}
			netDialer = dialer.Dial
		}
		s.conn, err = netDialer("tcp", destination)
	case streamTypeUDP:
		if s.wispConn.config.DisableUDP || s.wispConn.config.Proxy != "" {
			s.close(closeReasonBlocked)
			return
		}
		s.conn, err = netDialer("udp", destination)
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
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *wispStream) readFromConnection() {
	var closeReason uint8

	dataChan := make(chan []byte)
	defer close(dataChan)
	go func() {
		for data := range dataChan {
			s.sendData(data)
		}
	}()

	isEven := true
	buffer1 := make([]byte, s.wispConn.config.TcpBufferSize)
	buffer2 := make([]byte, s.wispConn.config.TcpBufferSize)

	for {
		var buffer []byte
		if isEven {
			buffer = buffer1
		} else {
			buffer = buffer2
		}

		n, err := s.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				closeReason = closeReasonVoluntary
			} else {
				closeReason = closeReasonNetworkError
			}
			break
		}

		dataChan <- buffer[:n]

		isEven = !isEven
	}

	s.close(closeReason)
}

func (s *wispStream) close(reason uint8) {
	s.isOpenMutex.Lock()
	defer s.isOpenMutex.Unlock()
	if !s.isOpen {
		return
	}
	s.isOpen = false

	s.wispConn.deleteWispStream(s.streamId)

	s.closeConnection()

	close(s.dataQueue)

	close(s.connEstablished)

	s.sendClose(reason)
}
