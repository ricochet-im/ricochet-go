package goricochet

import (
	"errors"
	"github.com/s-rah/go-ricochet/utils"
	"io"
	"net"
	"sync"
)

// Connect sets up a client ricochet connection to host e.g. qn6uo4cmsrfv4kzq.onion. If this
// function finished successfully then the connection can be assumed to
// be open and authenticated.
// To specify a local port using the format "127.0.0.1:[port]|ricochet-id".
func Connect(host string) (*OpenConnection, error) {
	networkResolver := utils.NetworkResolver{}
	conn, host, err := networkResolver.Resolve(host)

	if err != nil {
		return nil, err
	}

	return Open(conn, host)
}

// Open establishes a protocol session on an established net.Conn, and returns a new
// OpenConnection instance representing this connection. On error, the connection
// will be closed. This function blocks until version negotiation has completed.
// The application should call Process() on the returned OpenConnection to continue
// handling protocol messages.
func Open(conn net.Conn, remoteHostname string) (*OpenConnection, error) {
	oc, err := negotiateVersion(conn, true)
	if err != nil {
		conn.Close()
		return nil, err
	}
	oc.OtherHostname = remoteHostname
	return oc, nil
}

// Serve accepts incoming connections on a net.Listener, negotiates protocol,
// and calls methods of the ServiceHandler to handle inbound connections. All
// calls to ServiceHandler happen on the caller's goroutine. The listener can
// be closed at any time to close the service.
func Serve(ln net.Listener, handler ServiceHandler) error {
	defer ln.Close()

	connChannel := make(chan interface{})
	listenErrorChannel := make(chan error)

	go func() {
		var pending sync.WaitGroup
		for {
			conn, err := ln.Accept()
			if err != nil {
				// Wait for pending connections before returning an error; this
				// prevents abandoned goroutines when the outer loop stops reading
				// from connChannel.
				pending.Wait()
				listenErrorChannel <- err
				close(connChannel)
				return
			}

			pending.Add(1)
			go func() {
				defer pending.Done()
				oc, err := negotiateVersion(conn, false)
				if err != nil {
					conn.Close()
					connChannel <- err
				} else {
					connChannel <- oc
				}
			}()
		}
	}()

	var listenErr error
	for {
		select {
		case err := <-listenErrorChannel:
			// Remember error, wait for connChannel to close
			listenErr = err

		case result, ok := <-connChannel:
			if !ok {
				return listenErr
			}

			switch v := result.(type) {
			case *OpenConnection:
				handler.OnNewConnection(v)
			case error:
				handler.OnFailedConnection(v)
			}
		}
	}

	return nil
}

// Perform version negotiation on the connection, and create an OpenConnection if successful
func negotiateVersion(conn net.Conn, outbound bool) (*OpenConnection, error) {
	versions := []byte{0x49, 0x4D, 0x01, 0x01}

	// Outbound side of the connection sends a list of supported versions
	if outbound {
		if n, err := conn.Write(versions); err != nil || n < len(versions) {
			return nil, err
		}

		res := make([]byte, 1)
		if _, err := io.ReadAtLeast(conn, res, len(res)); err != nil {
			return nil, err
		}

		if res[0] != 0x01 {
			return nil, errors.New("unsupported protocol version")
		}
	} else {
		// Read version response header
		header := make([]byte, 3)
		if _, err := io.ReadAtLeast(conn, header, len(header)); err != nil {
			return nil, err
		}

		if header[0] != versions[0] || header[1] != versions[1] || header[2] < 1 {
			return nil, errors.New("invalid protocol response")
		}

		// Read list of supported versions (which is header[2] bytes long)
		versionList := make([]byte, header[2])
		if _, err := io.ReadAtLeast(conn, versionList, len(versionList)); err != nil {
			return nil, err
		}

		selectedVersion := byte(0xff)
		for _, v := range versionList {
			if v == 0x01 {
				selectedVersion = v
				break
			}
		}

		if n, err := conn.Write([]byte{selectedVersion}); err != nil || n < 1 {
			return nil, err
		}

		if selectedVersion == 0xff {
			return nil, errors.New("no supported protocol version")
		}
	}

	oc := new(OpenConnection)
	oc.Init(outbound, conn)
	return oc, nil
}
