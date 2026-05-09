package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
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
			// TLS ClientHello — wrap and return for http.Server
			tlsConn := tls.Server(&bufConn{Conn: conn, Reader: br}, dl.tlsCfg)
			return tlsConn, nil
		}

		// Plain HTTP — read request and serve via handler directly
		go func(rawConn net.Conn, reader *bufio.Reader) {
			defer rawConn.Close()
			for {
				req, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				req.RemoteAddr = rawConn.RemoteAddr().String()
				resp := &httpResponse{header: make(http.Header)}
				dl.handler.ServeHTTP(resp, req)
				resp.writeTo(rawConn)
				if resp.shouldClose() {
					return
				}
				reader.Reset(rawConn)
			}
		}(conn, br)
	}
}

type bufConn struct {
	net.Conn
	*bufio.Reader
}

func (bc *bufConn) Read(b []byte) (int, error) {
	return bc.Reader.Read(b)
}

// httpResponse collects the handler's response without using http.ResponseWriter directly.
type httpResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

func (r *httpResponse) Header() http.Header {
	return r.header
}

func (r *httpResponse) Write(data []byte) (int, error) {
	r.body = append(r.body, data...)
	return len(data), nil
}

func (r *httpResponse) WriteHeader(code int) {
	r.statusCode = code
}

func (r *httpResponse) writeTo(conn net.Conn) {
	if r.statusCode == 0 {
		r.statusCode = 200
	}
	text := http.StatusText(r.statusCode)
	if text == "" {
		text = "Unknown"
	}
	conn.Write([]byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n", r.statusCode, text)))
	for k, vs := range r.header {
		for _, v := range vs {
			conn.Write([]byte(fmt.Sprintf("%s: %s\r\n", k, v)))
		}
	}
	conn.Write([]byte(fmt.Sprintf("Content-Length: %d\r\n", len(r.body))))
	conn.Write([]byte("\r\n"))
	conn.Write(r.body)
}

func (r *httpResponse) shouldClose() bool {
	for _, v := range r.header["Connection"] {
		if v == "close" {
			return true
		}
	}
	return false
}
