package goricochet

import (
	"github.com/s-rah/go-ricochet/connection"
	"github.com/s-rah/go-ricochet/utils"
	"io"
	"net"
)

// Open establishes a protocol session on an established net.Conn, and returns a new
// OpenConnection instance representing this connection. On error, the connection
// will be closed. This function blocks until version negotiation has completed.
// The application should call Process() on the returned OpenConnection to continue
// handling protocol messages.
func Open(remoteHostname string) (*connection.Connection, error) {
	networkResolver := utils.NetworkResolver{}
	conn, remoteHostname, err := networkResolver.Resolve(remoteHostname)

	if err != nil {
		return nil, err
	}

	rc, err := NegotiateVersionOutbound(conn, remoteHostname)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return rc, nil
}

// negotiate version takes an open network connection and executes
// the ricochet version negotiation procedure.
func NegotiateVersionOutbound(conn net.Conn, remoteHostname string) (*connection.Connection, error) {
	versions := []byte{0x49, 0x4D, 0x01, 0x01}
	if n, err := conn.Write(versions); err != nil || n < len(versions) {
		return nil, utils.VersionNegotiationError
	}

	res := make([]byte, 1)
	if _, err := io.ReadAtLeast(conn, res, len(res)); err != nil {
		return nil, utils.VersionNegotiationError
	}

	if res[0] != 0x01 {
		return nil, utils.VersionNegotiationFailed
	}
	rc := connection.NewOutboundConnection(conn, remoteHostname)
	return rc, nil
}

// NegotiateVersionInbound takes in a connection and performs version negotiation
// as if that connection was a client. Returns a ricochet connection if successful
// error otherwise.
func NegotiateVersionInbound(conn net.Conn) (*connection.Connection, error) {
	versions := []byte{0x49, 0x4D, 0x01, 0x01}
	// Read version response header
	header := make([]byte, 3)
	if _, err := io.ReadAtLeast(conn, header, len(header)); err != nil {
		return nil, err
	}

	if header[0] != versions[0] || header[1] != versions[1] || header[2] < 1 {
		return nil, utils.VersionNegotiationError
	}

	// Read list of supported versions (which is header[2] bytes long)
	versionList := make([]byte, header[2])
	if _, err := io.ReadAtLeast(conn, versionList, len(versionList)); err != nil {
		return nil, utils.VersionNegotiationError
	}

	selectedVersion := byte(0xff)
	for _, v := range versionList {
		if v == 0x01 {
			selectedVersion = v
			break
		}
	}

	if n, err := conn.Write([]byte{selectedVersion}); err != nil || n < 1 {
		return nil, utils.VersionNegotiationFailed
	}

	if selectedVersion == 0xff {
		return nil, utils.VersionNegotiationFailed
	}

	rc := connection.NewInboundConnection(conn)
	return rc, nil
}
