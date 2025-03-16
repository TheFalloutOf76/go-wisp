package wisp

import (
	"net/http"

	"github.com/gorilla/websocket"
)

type Config struct {
	BufferRemainingLength uint32
	Blacklist             struct {
		Hostnames map[string]struct{}
	}
	DisableUDP    bool
	TcpBufferSize int
	TcpNoDelay    bool
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
			wsConn: wsConn,
			config: config,
		}
		defer wispConnection.deleteAllWispStreams()

		go wispConnection.sendContinuePacket(0, config.BufferRemainingLength)

		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				return
			}

			wispConnection.handlePacket(message)
		}
	}
}
