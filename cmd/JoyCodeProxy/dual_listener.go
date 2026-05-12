package main

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
)

// dualListener accepts both TLS and plain HTTP on the same port.
type dualListener struct {
	net.Listener
	tlsCfg  *tls.Config
	handler http.Handler
}

func newDualListener(ln net.Listener, tlsCfg *tls.Config, h http.Handler) *dualListener {
	return &dualListener{Listener: ln, tlsCfg: tlsCfg, handler: h}
}

func (dl *dualListener) Accept() (net.Conn, error) {
	for {
		conn, err := dl.Listener.Accept()
		if err != nil {
			return nil, err
		}

		br := bufio.NewReaderSize(conn, 1)
		b, err := br.Peek(1)
		if err != nil {
			conn.Close()
			continue
		}

		if b[0] == 0x16 {
			// TLS ClientHello — wrap and return for the main http.Server
			tlsConn := tls.Server(&bufConn{Conn: conn, Reader: br}, dl.tlsCfg)
			return tlsConn, nil
		}

		// Plain HTTP — serve via a dedicated http.Server for proper streaming support
		bc := &bufConn{Conn: conn, Reader: br}
		cl := &chanListener{
			ch:   make(chan net.Conn, 1),
			addr: conn.LocalAddr(),
		}
		cl.ch <- bc // deliver the connection

		go func() {
			srv := &http.Server{Handler: dl.handler}
			srv.Serve(cl) // serves the one conn, then Accept returns ErrServerClosed
			conn.Close()
		}()
	}
}

type bufConn struct {
	net.Conn
	*bufio.Reader
}

func (bc *bufConn) Read(b []byte) (int, error) {
	return bc.Reader.Read(b)
}

// chanListener delivers connections via a channel.
type chanListener struct {
	ch   chan net.Conn
	addr net.Addr
}

func (l *chanListener) Accept() (net.Conn, error) {
	conn, ok := <-l.ch
	if !ok {
		return nil, http.ErrServerClosed
	}
	return conn, nil
}

func (l *chanListener) Close() error {
	close(l.ch)
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }
