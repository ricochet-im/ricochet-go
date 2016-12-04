package utils

import (
	"errors"
	"golang.org/x/net/proxy"
	"net"
	"strings"
)

// NetworkResolver allows a client to resolve various hostnames to connections
// The supported types are onions address are:
//  * ricochet:jlq67qzo6s4yp3sp
//  * jlq67qzo6s4yp3sp
//  * 127.0.0.1:55555|jlq67qzo6s4yp3sp - Localhost Connection
type NetworkResolver struct {
}

// Resolve takes a hostname and returns a net.Conn to the derived endpoint
func (nr *NetworkResolver) Resolve(hostname string) (net.Conn, string, error) {
	if strings.HasPrefix(hostname, "127.0.0.1") {
		addrParts := strings.Split(hostname, "|")
		tcpAddr, err := net.ResolveTCPAddr("tcp", addrParts[0])
		if err != nil {
			return nil, "", errors.New("Cannot Resolve Local TCP Address")
		}
		conn, err := net.DialTCP("tcp", nil, tcpAddr)
		if err != nil {
			return nil, "", errors.New("Cannot Dial Local TCP Address")
		}

		// return just the onion address, not the local override for the hostname
		return conn, addrParts[1], nil
	}

	resolvedHostname := hostname
	if strings.HasPrefix(hostname, "ricochet:") {
		addrParts := strings.Split(hostname, ":")
		resolvedHostname = addrParts[1]
	}

	torDialer, err := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
	if err != nil {
		return nil, "", err
	}

	conn, err := torDialer.Dial("tcp", resolvedHostname+".onion:9878")
	if err != nil {
		return nil, "", errors.New("Cannot Dial Remote Ricochet Address")
	}
	//conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn, resolvedHostname, nil
}
