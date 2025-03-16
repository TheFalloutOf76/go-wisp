package wisp

import "encoding/binary"

func createWispPacket(packetType uint8, streamId uint32, payload []byte) []byte {
	packet := make([]byte, 5+len(payload))
	packet[0] = packetType
	binary.LittleEndian.PutUint32(packet[1:5], streamId)
	copy(packet[5:], payload)
	return packet
}

func parseWispPacket(packet []byte) (uint8, uint32, []byte) {
	packetType := packet[0]
	streamId := binary.LittleEndian.Uint32(packet[1:5])
	payload := make([]byte, len(packet)-5)
	copy(payload, packet[5:])
	return packetType, streamId, payload
}
