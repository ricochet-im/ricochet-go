package main

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

var (
	listeners     map[string]*InnerNetListener = make(map[string]*InnerNetListener)
	listenersSync sync.Mutex
)

// Compatible with net.Listener, but uses io.Pipe for in-process
// communication without any real sockets.
type InnerNetListener struct {
	connChannel chan *InnerNetConn
	connSync    sync.Mutex
	addr        InnerNetAddr
	closed      bool
}

type InnerNetAddr struct {
	addr string
}

type InnerNetConn struct {
	readPipe      *io.PipeReader
	writePipe     *io.PipeWriter
	localAddr     InnerNetAddr
	remoteAddr    InnerNetAddr
	readDeadline  time.Time
	writeDeadline time.Time
}

func ListenInnerNet(addr string) (*InnerNetListener, error) {
	listener := &InnerNetListener{
		connChannel: make(chan *InnerNetConn),
		addr:        InnerNetAddr{addr: addr},
	}

	listenersSync.Lock()
	defer listenersSync.Unlock()
	if _, exists := listeners[addr]; exists {
		return nil, errors.New("Server already exists")
	}
	listeners[addr] = listener

	return listener, nil
}

func DialInnerNet(addr string, timeout time.Duration) (net.Conn, error) {
	listenersSync.Lock()
	listener := listeners[addr]
	listenersSync.Unlock()
	if listener == nil {
		return nil, errors.New("Server does not exist")
	}

	var returnErr error
	result := make(chan *InnerNetConn)

	go func() {
		listener.connSync.Lock()
		defer listener.connSync.Unlock()
		if listener.closed {
			returnErr = errors.New("Connection refused")
			result <- nil
			return
		}

		clientRead, serverWrite := io.Pipe()
		serverRead, clientWrite := io.Pipe()
		client := &InnerNetConn{
			readPipe:   clientRead,
			writePipe:  clientWrite,
			remoteAddr: InnerNetAddr{addr: addr},
		}
		server := &InnerNetConn{
			readPipe:  serverRead,
			writePipe: serverWrite,
			localAddr: InnerNetAddr{addr: addr},
		}

		listener.connChannel <- server
		result <- client
	}()

	select {
	case re := <-result:
		return re, returnErr
	case <-time.After(timeout):
		return nil, errors.New("Connection timeout")
	}
}

func (l *InnerNetListener) Accept() (net.Conn, error) {
	if l.closed {
		return nil, errors.New("Closed")
	}

	conn, ok := <-l.connChannel
	if !ok {
		return nil, errors.New("Closed")
	}
	return conn, nil
}

func (l *InnerNetListener) Close() error {
	if l.closed {
		return nil
	}

	listenersSync.Lock()
	delete(listeners, l.addr.addr)
	listenersSync.Unlock()

	l.connSync.Lock()
	l.closed = true
	close(l.connChannel)
	l.connSync.Unlock()

	return nil
}

func (l *InnerNetListener) Addr() net.Addr {
	return l.addr
}

func (a InnerNetAddr) Network() string {
	return "innernet"
}

func (a InnerNetAddr) String() string {
	return a.addr
}

func (c *InnerNetConn) Read(b []byte) (int, error) {
	return c.readPipe.Read(b)
}

func (c *InnerNetConn) Write(b []byte) (int, error) {
	return c.writePipe.Write(b)
}

func (c *InnerNetConn) Close() error {
	c.writePipe.Close()
	c.readPipe.Close()
	return nil
}

func (c *InnerNetConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *InnerNetConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *InnerNetConn) SetDeadline(t time.Time) error {
	return errors.New("Not implemented")
}

func (c *InnerNetConn) SetReadDeadline(t time.Time) error {
	return errors.New("Not implemented")
}

func (c *InnerNetConn) SetWriteDeadline(t time.Time) error {
	return errors.New("Not implemented")
}
