package wisp

// packet types
const (
	packetTypeConnect  uint8 = 0x01
	packetTypeData     uint8 = 0x02
	packetTypeContinue uint8 = 0x03
	packetTypeClose    uint8 = 0x04
)

// stream types
const (
	streamTypeTCP uint8 = 0x01
	streamTypeUDP uint8 = 0x02
)

// close reasons (client/server)
const (
	closeReasonUnspecified  uint8 = 0x01
	closeReasonVoluntary    uint8 = 0x02
	closeReasonNetworkError uint8 = 0x03
)

// close reasons (server only)
const (
	closeReasonInvalidInfo       uint8 = 0x41
	closeReasonUnreachable       uint8 = 0x42
	closeReasonTimeout           uint8 = 0x43
	closeReasonConnectionRefused uint8 = 0x44
	closeReasonTCPTimeout        uint8 = 0x47
	closeReasonBlocked           uint8 = 0x48
	closeReasonThrottled         uint8 = 0x49
)

// close reasons (client only)
const (
	closeReasonClientError uint8 = 0x81
)
