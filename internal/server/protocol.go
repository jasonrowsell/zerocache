package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/jasonrowsell/zerocache/pkg/protocol"
)

type Command struct {
	Type  uint8
	Key   string
	Value []byte // Only used for SET
}

// Name returns human-readable name for the command type.
func (c *Command) Name() string {
	switch c.Type {
	case protocol.CmdSet:
		return "SET"
	case protocol.CmdGet:
		return "GET"
	case protocol.CmdDel:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

type Response struct {
	Type  uint8
	Value []byte
}

// Pool for temporary buffers used in ReadCommand/WriteResponse
// Assuming max header + typical key/value might fit in 1KB often.
var bufferPool = sync.Pool{
	New: func() any {
		// Allocate a buffer of 1KB default size.
		b := make([]byte, 1024)
		return &b // Return pointer to slice
	},
}

// ReadCommand reads from the reader and parses a command according to the protocol.
func ReadCommand(r io.Reader) (*Command, error) {
	var header [9]byte // 1 (Cmd) + 4 (KeyLen) + 4 (ValLen)

	if _, err := io.ReadFull(r, header[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read command header: %w", err)
	}

	cmdType := header[0]
	keyLen := binary.BigEndian.Uint32(header[1:5])
	valLen := binary.BigEndian.Uint32(header[5:9])

	if keyLen == 0 || keyLen > protocol.MaxKeySize {
		return nil, fmt.Errorf("invalid key length: %d, (max %d)", keyLen, protocol.MaxKeySize)
	}
	if valLen > protocol.MaxValueSize {
		return nil, fmt.Errorf("invalid value length: %d, (max %d)", valLen, protocol.MaxValueSize)
	}
	if valLen > 0 && (cmdType == protocol.CmdGet || cmdType == protocol.CmdDel) {
		return nil, fmt.Errorf("protocol violation: value data sent for non-SET command (type %d)", cmdType)
	}

	cmd := &Command{Type: cmdType}

	totalPayloadLen := keyLen + valLen // valLen is 0 for GET/DEL
	var payloadBufPtr *[]byte
	var payloadBuf []byte // Holds key (and potentially value for SET)

	// Get buffer from pool
	payloadBufPtr = bufferPool.Get().(*[]byte)
	// Only need buffer size for key if not SET, else key+value
	neededSize := int(keyLen)
	if cmdType == protocol.CmdSet {
		neededSize = int(totalPayloadLen)
	}

	if cap(*payloadBufPtr) < neededSize {
		bufferPool.Put(payloadBufPtr) // Put back small one
		payloadBuf = make([]byte, neededSize)
	} else {
		// Use buffer from pool, slice it to the needed length
		payloadBuf = (*payloadBufPtr)[:neededSize]
	}
	// Ensure buffer is put back when done
	defer func() {
		bufferPool.Put(payloadBufPtr)
	}()

	if neededSize > 0 {
		if _, err := io.ReadFull(r, payloadBuf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("eof while reading key: %w", err)
			}
			return nil, fmt.Errorf("failed to read key data: %w", err)
		}
	}

	// Extract key and value from the buffer
	cmd.Key = string(payloadBuf[:keyLen])
	if cmdType == protocol.CmdSet {
		// Copy value from buffer into the command struct
		// Cache needs to own its copy
		cmd.Value = make([]byte, valLen)
		copy(cmd.Value, payloadBuf[keyLen:totalPayloadLen])
	}

	switch cmdType {
	case protocol.CmdSet, protocol.CmdGet, protocol.CmdDel:
		// Valid
	default:
		return nil, fmt.Errorf("unknown command type: %d", cmdType)
	}

	return cmd, nil
}

// WriteResponse formats and writes a response to the writer.
func WriteResponse(w io.Writer, resp *Response) error {
	valLen := uint32(len(resp.Value))
	headerLen := 5

	if valLen > protocol.MaxValueSize {
		errMsg := "internal: response value exceeds maximum size"
		if len(errMsg) > protocol.MaxValueSize {
			errMsg = errMsg[:protocol.MaxValueSize] // Truncate
		}
		errResp := &Response{
			Type:  protocol.RespError,
			Value: []byte(errMsg),
		}

		totalLen := headerLen + len(errResp.Value)
		buf := make([]byte, totalLen)
		buf[0] = errResp.Type
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(errResp.Value)))
		copy(buf[headerLen:], errResp.Value)
		if _, writeErr := w.Write(buf); writeErr != nil {
			return fmt.Errorf("failed to write truncated error response: %w", writeErr)
		}
		return fmt.Errorf("original response value exceeds maximum size")
	}
	if (resp.Type == protocol.RespOK || resp.Type == protocol.RespNotFound) && valLen != 0 {
		errMsg := fmt.Sprintf("internal: unexpected value data with response type %d", resp.Type)
		errResp := &Response{Type: protocol.RespError, Value: []byte(errMsg)}
		totalLen := headerLen + len(errResp.Value)
		buf := make([]byte, totalLen)
		buf[0] = errResp.Type
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(errResp.Value)))
		copy(buf[headerLen:], errResp.Value)
		if _, writeErr := w.Write(buf); writeErr != nil {
			return fmt.Errorf("failed to write internal protocol error response: %w", writeErr)
		}
		return fmt.Errorf("internal server error: tried to send data with OK/NotFound")
	}

	totalLen := headerLen + int(valLen)

	// Get buffer from pool
	bufPtr := bufferPool.Get().(*[]byte)
	var buf []byte
	if cap(*bufPtr) < totalLen {
		buf = make([]byte, totalLen) // Put small one back
	} else {
		buf = (*bufPtr)[:totalLen]
	}
	defer func() {
		bufferPool.Put(bufPtr)
	}()

	// Write header and value into the pooled buffer
	buf[0] = resp.Type
	binary.BigEndian.PutUint32(buf[1:5], valLen)
	if valLen > 0 {
		copy(buf[headerLen:], resp.Value)
	}

	// Write the entire buffer in one go
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("failed to write response buffer: %w", err)
	}

	return nil
}

// WriteError is a helper function to write an error response.
func WriteError(w io.Writer, errMsg string) error {
	if len(errMsg) > protocol.MaxValueSize {
		errMsg = errMsg[:protocol.MaxValueSize] // Truncate
	}
	resp := &Response{Type: protocol.RespError, Value: []byte(errMsg)}

	return WriteResponse(w, resp)
}
