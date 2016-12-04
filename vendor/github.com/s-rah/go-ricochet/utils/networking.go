package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// RicochetData is a structure containing the raw data and the channel it the
// message originated on.
type RicochetData struct {
	Channel int32
	Data    []byte
}

//Equals compares a RicochetData object to another and returns true if contain
// the same data.
func (rd RicochetData) Equals(other RicochetData) bool {
	return rd.Channel == other.Channel && bytes.Equal(rd.Data, other.Data)
}

// RicochetNetworkInterface abstract operations that interact with ricochet's
// packet layer.
type RicochetNetworkInterface interface {
	SendRicochetPacket(dst io.Writer, channel int32, data []byte) error
	RecvRicochetPacket(reader io.Reader) (RicochetData, error)
}

// RicochetNetwork is a concrete implementation of the RicochetNetworkInterface
type RicochetNetwork struct {
}

// SendRicochetPacket places the data into a structure needed for the client to
// decode the packet and writes the packet to the network.
func (rn *RicochetNetwork) SendRicochetPacket(dst io.Writer, channel int32, data []byte) error {
	packet := make([]byte, 4+len(data))
	if len(packet) > 65535 {
		return errors.New("packet too large")
	}
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	if channel < 0 || channel > 65535 {
		return errors.New("invalid channel ID")
	}
	binary.BigEndian.PutUint16(packet[2:4], uint16(channel))
	copy(packet[4:], data[:])

	for pos := 0; pos < len(packet); {
		n, err := dst.Write(packet[pos:])
		if err != nil {
			return err
		}
		pos += n
	}
	return nil
}

// RecvRicochetPacket returns the next packet from reader as a RicochetData
// structure, or an error.
func (rn *RicochetNetwork) RecvRicochetPacket(reader io.Reader) (RicochetData, error) {
	packet := RicochetData{}

	// Read the four-byte header to get packet length
	header := make([]byte, 4)
	if _, err := io.ReadAtLeast(reader, header, len(header)); err != nil {
		return packet, err
	}

	size := int(binary.BigEndian.Uint16(header[0:2]))
	if size < 4 {
		return packet, errors.New("invalid packet length")
	}

	packet.Channel = int32(binary.BigEndian.Uint16(header[2:4]))
	packet.Data = make([]byte, size-4)

	if _, err := io.ReadAtLeast(reader, packet.Data, len(packet.Data)); err != nil {
		return packet, err
	}

	return packet, nil
}
