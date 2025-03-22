package wisp

import (
	"net/http"

	"github.com/lxzan/gws"
)

type Config struct {
	BufferRemainingLength uint32
	Blacklist             struct {
		Hostnames map[string]struct{}
	}
	DisableUDP          bool
	TcpBufferSize       int
	TcpNoDelay          bool
	WebsocketTcpNoDelay bool
}

func CreateWispHandler(config *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler := &handler{}

		upgrader := gws.NewUpgrader(handler, &gws.ServerOption{
			PermessageDeflate: gws.PermessageDeflate{Enabled: true},
		})

		wsConn, err := upgrader.Upgrade(w, r)
		if err != nil {
			return
		}

		wsConn.SetNoDelay(config.WebsocketTcpNoDelay)

		handler.wispConn = &wispConnection{
			wsConn: wsConn,
			config: config,
		}

		go wsConn.ReadLoop()
	}
}

type handler struct {
	gws.BuiltinEventHandler
	wispConn *wispConnection
}

func (h *handler) OnOpen(socket *gws.Conn) {
	h.wispConn.init()
}

func (h *handler) OnClose(socket *gws.Conn, err error) {
	h.wispConn.deleteAllWispStreams()
}

func (h *handler) OnMessage(socket *gws.Conn, message *gws.Message) {
	defer message.Close()

	packet := message.Bytes()
	if len(packet) < 5 {
		return
	}
	packetType, streamId, payload := parseWispPacket(packet)

	h.wispConn.handlePacket(packetType, streamId, payload)
}
