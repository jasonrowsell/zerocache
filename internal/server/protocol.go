package server

import (
	"encoding/binary"
	"fmt"
	"io"

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
	if valLen == 0 || valLen > protocol.MaxValueSize {
		return nil, fmt.Errorf("invalid value length: %d, (max %d)", valLen, protocol.MaxValueSize)
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
	if cmdType == protocol.CmdSet && valLen > 0 {
		cmd.Value = make([]byte, valLen)
		if _, err := io.ReadFull(r, cmd.Value); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("eof while reading value: %w", err)
			}
			return nil, fmt.Errorf("failed to read value data: %w", err)
		}
	} else if cmdType == protocol.CmdSet && valLen == 0 {
		// Handle setting empty value explicitly
		cmd.Value = []byte{}
	} else if valLen > 0 && (cmdType == protocol.CmdGet || cmdType == protocol.CmdDel) {
		// Client sent value data for GET/DEL, protocol violation
		// Discard these bytes to not desync the stream
		return nil, fmt.Errorf("procoal violation: value data sent for %s command", cmd.Name())
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

	if valLen > protocol.MaxValueSize {
		errResp := &Response{
			Type:  protocol.RespError,
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
	resp := &Response{Type: protocol.RespError, Value: []byte(errMsg)}

	return WriteResponse(w, resp)
}
