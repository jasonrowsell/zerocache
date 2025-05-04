package client

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/jasonrowsell/zerocache/pkg/protocol"
)

type Error string

func (e Error) Error() string {
	return string(e)
}

var ErrNotFound = Error("key not found")

type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	addr   string
	// Mutex to protect concurrent use of the same client connection
	// for sending/receiving.
	mu sync.Mutex
}

var clientBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 1024)
		return &b
	},
}

func New(addr string) (*Client, error) {
	connTimeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", addr, connTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	return NewWithConn(conn)
}

// NewWithConn creates a new client using an existing network connection.
// It assumes the caller might have already configured the connection (e.g., deadlines, options).
// It ensures TCP_NODELAY is set if it's a TCP connection.
func NewWithConn(conn net.Conn) (*Client, error) {
	if conn == nil {
		return nil, fmt.Errorf("cannot create client with nil connection")
	}

	// Ensure TCP_NODELAY is set for low latency, idempotent if already set.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	addrStr := "unknown"
	if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
		addrStr = remoteAddr.String()
	}

	return &Client{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
		addr:   addrStr,
	}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil // Already closed
	}
	err := c.conn.Close()
	c.conn = nil // Mark as clsed
	return err
}

// sendCommand serializes and sends a command. Assumes lock is held.
func (c *Client) sendCommand(cmdType uint8, key string, value []byte) error {
	keyLen := len(key)
	valLen := len(value) // Will be 0 if value is nil/empty

	// Prepare header + key + value buffer
	bufSize := 1 + 4 + 4 + keyLen + valLen
	bufPtr := clientBufferPool.Get().(*[]byte)
	var buf []byte
	if cap(*bufPtr) < bufSize {
		clientBufferPool.Put(bufPtr) // Put back small one
		buf = make([]byte, bufSize)
	} else {
		buf = (*bufPtr)[:bufSize]
	}
	defer clientBufferPool.Put(bufPtr) // Put back when done

	buf[0] = cmdType
	binary.BigEndian.PutUint32(buf[1:5], uint32(keyLen))
	binary.BigEndian.PutUint32(buf[5:9], uint32(valLen))
	copy(buf[9:9+keyLen], key)
	if valLen > 0 {
		copy(buf[9+keyLen:], value)
	}

	// Write the entire command
	if _, err := c.writer.Write(buf); err != nil {
		c.closeConnOnError(err)
		return fmt.Errorf("write error: %w", err)
	}

	// Flush the writer buffer
	if err := c.writer.Flush(); err != nil {
		c.closeConnOnError(err)
		return fmt.Errorf("flush error: %w", err)
	}
	return nil
}

// Set sends a SET command to the server.
func (c *Client) Set(key string, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("client closed")
	}
	if len(key) == 0 || len(key) > protocol.MaxKeySize {
		return fmt.Errorf("invalid key length")
	}
	if len(value) > protocol.MaxValueSize {
		return fmt.Errorf("invalid value length")
	} // len=0 is OK

	if err := c.sendCommand(protocol.CmdSet, key, value); err != nil {
		return err
	}
	respType, respValue, err := c.readResponse()
	if err != nil {
		return err
	}

	switch respType {
	case protocol.RespOK:
		return nil
	case protocol.RespError:
		return Error(respValue)
	default:
		err = fmt.Errorf("protocol error: unexpected response type %d for SET", respType)
		c.closeConnOnError(err)
		return err
	}
}

// Set sends a GET command to the server.
func (c *Client) Get(key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("client closed")
	}
	if len(key) == 0 || len(key) > protocol.MaxKeySize {
		return nil, fmt.Errorf("invalid key length")
	}

	if err := c.sendCommand(protocol.CmdGet, key, nil); err != nil {
		return nil, err
	}

	respType, respValue, err := c.readResponse()
	if err != nil {
		return nil, err
	}

	switch respType {
	case protocol.RespValue:
		return respValue, nil
	case protocol.RespNotFound:
		return nil, ErrNotFound
	case protocol.RespError:
		return nil, Error(respValue)
	default:
		err = fmt.Errorf("protocol error: unexpected response type %d for GET", respType)
		c.closeConnOnError(err)
		return nil, err
	}

}

// Set sends a DELETE command to the server.
func (c *Client) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("client closed")
	}
	if len(key) == 0 || len(key) > protocol.MaxKeySize {
		return fmt.Errorf("invalid key length")
	}

	if err := c.sendCommand(protocol.CmdDel, key, nil); err != nil {
		return err
	}

	respType, respValue, err := c.readResponse()
	if err != nil {
		return err
	}

	switch respType {
	case protocol.RespOK:
		return nil
	case protocol.RespError:
		return Error(respValue)
	default:
		err = fmt.Errorf("protocol error: unexpected response type %d for DELETE", respType)
		c.closeConnOnError(err)
		return err
	}
}

// readResponse reads and parses the response header and body. Assumes lock is held.
// It returns the response type code, the value (if applicable), and any error encountered.
func (c *Client) readResponse() (respType uint8, value []byte, err error) {
	var header [5]byte // 1 (RespType) + 4 (ValLen)

	// Read the fixed-size header
	if _, err = io.ReadFull(c.reader, header[:]); err != nil {
		// If an error occurs reading the header (e.g., connection closed, timeout),
		// mark the connection as potentially unusable and return the error.
		c.closeConnOnError(err)
		return 0, nil, fmt.Errorf("read header error: %w", err)
	}

	// Parse the header fields
	respType = header[0]
	valLen := binary.BigEndian.Uint32(header[1:5])

	if (respType == protocol.RespOK || respType == protocol.RespNotFound) && valLen != 0 {
		err = fmt.Errorf("protocol error: unexpected non-zero length %d for response type %d", valLen, respType)
		c.closeConnOnError(err)
		return respType, nil, err
	}
	if valLen > protocol.MaxValueSize {
		err = fmt.Errorf("protocol error: response value length %d exceeds client maximum %d", valLen, protocol.MaxValueSize)
		c.closeConnOnError(err)
		return respType, nil, err
	}

	if valLen > 0 {
		// Get a buffer from the pool for reading the value data.
		bufPtr := clientBufferPool.Get().(*[]byte)
		var readBuf []byte
		if cap(*bufPtr) < int(valLen) {
			// Pooled buffer is too small; put it back and allocate a new one.
			clientBufferPool.Put(bufPtr)
			readBuf = make([]byte, valLen)
		} else {
			// Pooled buffer is large enough; slice it to the exact size needed.
			readBuf = (*bufPtr)[:valLen]
		}
		// Ensure the read buffer is returned to the pool when this function exits.
		defer clientBufferPool.Put(bufPtr)

		// Read the value data fully into the readBuf.
		if _, err = io.ReadFull(c.reader, readBuf); err != nil {
			c.closeConnOnError(err)
			return respType, nil, fmt.Errorf("read value error: %w", err)
		}

		// Copy data from the pooled buffer.
		// We cannot return `readBuf` directly, as it belongs to the pool and will be reused.
		// We must allocate a new slice (`value`) and copy the data into it.
		value = make([]byte, valLen)
		copy(value, readBuf)

	} // else valLen is 0, so `value` remains nil (or its zero value, an empty slice)

	return respType, value, nil
}

// closeConnOnError closes the connection and marks the client as closed when a fatal error occurs.
// Assumes lock is already held or not needed (e.g., called from defer).
func (c *Client) closeConnOnError(err error) {
	// Check for common "connection closed" errors to avoid redundant closing
	if c.conn != nil && err != nil && err != io.EOF && !isConnClosedError(err) {
		log.Printf("Client connection error (%v), closing connection to %s", err, c.addr)
		c.conn.Close()
		c.conn = nil
	} else if c.conn != nil && err == io.EOF {
		// EOF might just mean peer closed gracefully, mark as closed
		c.conn.Close()
		c.conn = nil
	}
}

// isConnClosedError checks for common network errors indicating closure.
func isConnClosedError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common net package errors related to closed connections
	if opErr, ok := err.(*net.OpError); ok {
		return opErr.Err.Error() == "use of closed network connection" ||
			opErr.Err.Error() == "connection reset by peer" ||
			opErr.Err.Error() == "broken pipe"
	}
	return err == net.ErrClosed
}
