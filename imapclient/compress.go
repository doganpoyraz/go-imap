package imapclient

import (
	"bufio"
	"bytes"
	"compress/flate"
	"io"
	"net"
)

// CompressDeflateOptions contains options for Client.CompressDeflate.
type CompressDeflateOptions struct{}

// CompressDeflate enables connection-level compression.
//
// Unlike other commands, this method blocks until the command completes.
//
// A nil options pointer is equivalent to a zero options value.
func (c *Client) CompressDeflate(options *CompressDeflateOptions) error {
	upgradeDone := make(chan struct{})
	cmd := &compressCommand{
		upgradeDone: upgradeDone,
	}
	enc := c.beginCommand("COMPRESS", cmd)
	enc.SP().Atom("DEFLATE")
	enc.flush()
	defer enc.end()

	// The client MUST NOT send any further commands until it has seen the
	// result of COMPRESS.

	if err := cmd.Wait(); err != nil {
		return err
	}

	// The decoder goroutine will invoke Client.upgradeCompress
	<-upgradeDone
	return nil
}

func (c *Client) upgradeCompress() {
	// Drain buffered data from our bufio.Reader
	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, c.br, int64(c.br.Buffered())); err != nil {
		panic(err) // unreachable
	}

	// TODO: wrap TLS conn
	var r io.Reader
	if buf.Len() > 0 {
		r = io.MultiReader(&buf, c.conn)
	} else {
		r = c.conn
	}

	w, err := flate.NewWriter(c.conn, flate.DefaultCompression)
	if err != nil {
		panic(err) // can only happen due to bad arguments
	}

	conn := &compressConn{
		Conn: c.conn,
		r:    flate.NewReader(r),
		w:    w,
	}
	rw := c.options.wrapReadWriter(conn)

	c.br.Reset(rw)
	// Unfortunately we can't re-use the bufio.Writer here, it races with
	// Client.CompressDeflate
	c.bw = bufio.NewWriter(rw)
}

type compressCommand struct {
	cmd
	upgradeDone chan<- struct{}
}

type compressConn struct {
	net.Conn
	r io.ReadCloser
	w *flate.Writer
}

func (conn *compressConn) Read(b []byte) (int, error) {
	return conn.r.Read(b)
}

func (conn *compressConn) Write(b []byte) (int, error) {
	return conn.w.Write(b)
}

func (conn *compressConn) Close() error {
	if err := conn.r.Close(); err != nil {
		return err
	}
	if err := conn.w.Close(); err != nil {
		return err
	}
	return conn.Conn.Close()
}
