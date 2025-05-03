package server

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Command types
const (
	CmdSet uint8 = 1
	CmdGet uint8 = 2
	CmdDel uint8 = 3
)

// Response types
const (
	RespOK       uint8 = 1 // Generic OK
	RespError    uint8 = 2 // Error message follows
	RespValue    uint  = 3 // Value data follows
	RespNotfound uint8 = 4 // Key not found (specific to GET)
)

type Command struct {
	Type  uint8
	Key   string
	Value []byte // Only used for SET
}

// Name returns human-readable name for the command type.
func (c *Command) Name() string {
	switch c.Type {
	case CmdSet:
		return "SET"
	case CmdGet:
		return "GET"
	case CmdDel:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

type Response struct {
	Type  uint8
	Value []byte
}

const maxKeySize = 1028        // 1KB limit for keys
const maxValueSize = 64 * 1028 // 64KB limit for values

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

	if keyLen == 0 || keyLen > maxKeySize {
		return nil, fmt.Errorf("invalid key length: %d, (max %d)", keyLen, maxKeySize)
	}
	if valLen == 0 || valLen > maxValueSize {
		return nil, fmt.Errorf("invalid key length: %d, (max %d)", keyLen, maxKeySize)
	}

	cmd := &Command{Type: cmdType}

	// Read Key (allocate buffer once)
	keyBuf := make([]byte, keyLen)
	if _, err := io.ReadFull(r, keyBuf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("eof while reading key: %w", err)
		}
		return nil, fmt.Errorf("failed to read key data: %w", err)
	}
	cmd.Key = string(keyBuf)

	// Read Value only if needed (SET cmd)
	if cmdType == CmdSet && valLen > 0 {
		cmd.Value = make([]byte, valLen)
		if _, err := io.ReadFull(r, cmd.Value); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("eof while reading value: %w", err)
			}
			return nil, fmt.Errorf("failed to read value data: %w", err)
		}
	} else if cmdType == CmdSet && valLen == 0 {
		// Handle setting empty value explicitly
		cmd.Value = []byte{}
	} else if valLen > 0 && (cmdType == CmdGet || cmdType == CmdDel) {
		// Client sent value data for GET/DEL, protocol violation
		// Discard these bytes to not desync the stream
		return nil, fmt.Errorf("procoal violation: value data sent for %s command", cmd.Name())
	}

	switch cmdType {
	case CmdSet, CmdGet, CmdDel:
		// Valid
	default:
		return nil, fmt.Errorf("unknown command type: %d", cmdType)
	}

	return cmd, nil
}

// WriteResponse formats and writes a response to the writer.
func WriteResponse(w io.Writer, resp *Response) error {
	valLen := uint32(len(resp.Value))

	if valLen > maxValueSize {
		errResp := &Response{
			Type:  RespError,
			Value: []byte("internal: response value exceeds maximum limit"),
		}
		return WriteResponse(w, errResp) // Recursive call with the error response
	}

	var header [5]byte // 1 (RespType) + 4 (ValLen)
	header[0] = resp.Type
	binary.BigEndian.PutUint32(header[1:5], valLen)

	// Write header
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("failed to write response header: %w", err)
	}

	if valLen > 0 {
		if _, err := w.Write(resp.Value); err != nil {
			return fmt.Errorf("failed to write response value/error: %w", err)
		}
	}

	return nil
}

// WriteError is a helper function to write an error response.
func WriteError(w io.Writer, errMsg string) error {
	resp := &Response{Type: RespError, Value: []byte(errMsg)}

	return WriteResponse(w, resp)
}
