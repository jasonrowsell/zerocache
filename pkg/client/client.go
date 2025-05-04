package client

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

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
	// Simple mutex to protect concurrent use of the same client connection
	// for sending/receiving.
	mu sync.Mutex
}

// New connects to a ZeroCache server.
func New(addr string) (*Client, error) {
	// TODO: Add conn timeout
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	return &Client{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
		addr:   addr,
	}, nil
}

// Close closes the connection to the server.
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

// Set sends a SET command to the server.
func (c *Client) Set(key string, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("client closed")
	}

	keyLen := len(key)
	valLen := len(value)

	if keyLen == 0 || keyLen > protocol.MaxKeySize {
		return fmt.Errorf("invalid key length: %d", keyLen)
	}
	if valLen > protocol.MaxValueSize {
		// TODO: Maybe close connection?
		// For now, return generic error
		return fmt.Errorf("invalid value length: %d", valLen)
	}

	// Prepare header + key + value buffer (avoid multiple writes)
	// Size: 1 (Cmd) + 4 (KeyLen) + 4 (ValLen) + KeyLen + ValLen
	bufSize := 1 + 4 + 4 + keyLen + valLen
	buf := make([]byte, bufSize) // TODO: sync.Pool

	buf[0] = protocol.CmdSet
	binary.BigEndian.PutUint32(buf[1:5], uint32(keyLen))
	binary.BigEndian.PutUint32(buf[5:9], uint32(valLen))
	copy(buf[9:9+keyLen], key)
	copy(buf[9+keyLen:], value)

	if _, err := c.writer.Write(buf); err != nil {
		// TODO: Handle connection errors more robustly (e.g., reconnect?)
		return fmt.Errorf("failed to write set command: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	respType, _, err := c.readResponseHeader()
	if err != nil {
		return fmt.Errorf("failed to read set response header: %w", err)
	}

	switch respType {
	case protocol.RespOK:
		return nil
	case protocol.RespError:
		errMsg, err := c.readResponseValue(err, uint32(valLen))
		if err != nil {
			return fmt.Errorf("failed to read error response body: %w", err)
		}
		return Error(errMsg)
	default:
		return fmt.Errorf("protocol error: expected response type %d for SET", respType)
	}
}

// Set sends a GET command to the server.
func (c *Client) Get(key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("client closed")
	}

	keyLen := len(key)
	if keyLen == 0 || keyLen > protocol.MaxKeySize {
		return nil, fmt.Errorf("invalid key length: %d", keyLen)
	}

	// Prepare header + key buffer
	// Size: 1 (Cmd) + 4 (KeyLen) + 4 (ValLen=0) + KeyLen
	bufSize := 1 + 4 + 4 + keyLen
	buf := make([]byte, bufSize) // TODO: Use sync.Pool

	buf[0] = protocol.CmdGet
	binary.BigEndian.PutUint32(buf[1:5], uint32(keyLen))
	binary.BigEndian.PutUint32(buf[5:9], 0) // Value length is 0 for GET
	copy(buf[9:9+keyLen], key)

	if _, err := c.writer.Write(buf); err != nil {
		return nil, fmt.Errorf("failed to write get command: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush writer: %w", err)
	}

	respType, valLen, err := c.readResponseHeader()
	if err != nil {
		return nil, fmt.Errorf("failed to read get response header: %w", err)
	}

	switch respType {
	case protocol.RespValue:
		value, err := c.readResponseValue(err, valLen)
		if err != nil {
			return nil, fmt.Errorf("failed to read value response body: %w", err)
		}
		return value, nil
	case protocol.RespNotfound:
		if valLen > 0 {
			return nil, fmt.Errorf("protocol error: received NotFound with value length %d", valLen)
		}
		return nil, ErrNotFound
	case protocol.RespError:
		errMsg, err := c.readResponseValue(err, valLen)
		if err != nil {
			return nil, fmt.Errorf("failed to read error response body: %w", err)
		}
		return nil, Error(errMsg)
	default:
		return nil, fmt.Errorf("protocol error: unexpected response type %d for GET", respType)
	}
}

// Set sends a DELETE command to the server.
func (c *Client) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("client closed")
	}

	keyLen := len(key)
	if keyLen == 0 || keyLen > protocol.MaxKeySize {
		return fmt.Errorf("invalid key length: %d", keyLen)
	}

	// Prepare header + key buffer
	// Size: 1 (Cmd) + 4 (KeyLen) + 4 (ValLen=0) + KeyLen
	bufSize := 1 + 4 + 4 + keyLen
	buf := make([]byte, bufSize) // TODO: Use sync.Pool

	buf[0] = protocol.CmdDel
	binary.BigEndian.PutUint32(buf[1:5], uint32(keyLen))
	binary.BigEndian.PutUint32(buf[5:9], 0) // Value length is 0 for DEL
	copy(buf[9:9+keyLen], key)

	if _, err := c.writer.Write(buf); err != nil {
		return fmt.Errorf("failed to write delete command: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	respType, valLen, err := c.readResponseHeader()
	if err != nil {
		return fmt.Errorf("failed to read delete response header: %w", err)
	}

	switch respType {
	case protocol.RespOK:
		if valLen > 0 {
			return fmt.Errorf("protocol error: received OK with value length %d", valLen)
		}
		return nil
	case protocol.RespError:
		errMsg, err := c.readResponseValue(err, uint32(valLen))
		if err != nil {
			return fmt.Errorf("failed to read error response body: %w", err)
		}
		return Error(errMsg)
	default:
		return fmt.Errorf("protocol error: expected response type %d for DELETE", respType)
	}
}

// readResponseHeader reads the common 5-byte header.
// Returns response, value length, and error.
func (c *Client) readResponseHeader() (uint8, uint32, error) {
	var header [5]byte // 1 (RespType) + 4 (ValLen)

	// Ensure we have all 5 bytes or an error (e.g., EOF)
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, 0, fmt.Errorf("connection error reading header: %w", err)
	}

	respType := header[0]
	valLen := binary.BigEndian.Uint32(header[1:5])

	if (respType == protocol.RespOK || respType == protocol.RespNotfound) && valLen != 0 {
		return respType, valLen, fmt.Errorf("protocol error: unexpected non-zero length %d for response type %d", valLen, respType)
	}
	if valLen > protocol.MaxValueSize {
		return respType, valLen, fmt.Errorf("protocol error: response value length %d exceeds maximum %d", valLen, protocol.MaxValueSize)
	}

	return respType, valLen, nil
}

// readResponseValue reads the value/error message part of a response, given the length from the header.
// The 'headerErr' is passed in case the caller already encountered an error reading the header,
// in which case we just return that error.
func (c *Client) readResponseValue(headerErr error, valLen uint32) ([]byte, error) {
	if headerErr != nil {
		return nil, headerErr
	}
	if valLen == 0 {
		return []byte{}, nil
	}

	valueBuf := make([]byte, valLen) // TODO: Use sync.Pool
	if _, err := io.ReadFull(c.reader, valueBuf); err != nil {
		return nil, fmt.Errorf("connection error reading value body: %w", err)
	}

	return valueBuf, nil
}
