package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/jasonrowsell/zerocache/internal/cache"
	"github.com/jasonrowsell/zerocache/pkg/protocol"
)

// Server holds the dependencies for the ZeroCache server.
type Server struct {
	cache    *cache.Cache
	wg       sync.WaitGroup
	shutdown chan struct{}
}

func New(c *cache.Cache) *Server {
	return &Server{
		cache:    c,
		shutdown: make(chan struct{}),
	}
}

// ListenAndServe starts the TCP server and listens for incoming connections.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer listener.Close()
	log.Printf("ZeroCache server listening on %s", addr)

	for {
		conn, err := listener.Accept()

		if err != nil {
			select {
			case <-s.shutdown:
				return nil // Graceful shutdown initiated
			default:
			}

			if _, ok := err.(net.Error); ok {
				log.Printf("Temporary accept error: %v; retrying...", err)
				continue
			}
			log.Printf("Permanent accept error: %v; stopping listener", err)
			return err
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
		}

		s.wg.Add(1)

		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConnection(conn)
		}(conn)
	}
}

func (s *Server) Shutdown() {
	close(s.shutdown) // Signal listener to stop accepting

	s.wg.Wait() // Wait for all active connections to finish
	log.Println("Server connections closed.")
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Use bufio for potentially better performance with buffered I/O
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		// 1. Read and Parse Command (using our custom protocol)
		cmd, err := ReadCommand(reader)
		if err != nil {
			if err != io.EOF {
				// EOF is expected when client disconnects gracefully
				log.Printf("Error reading command from %s: %v", conn.RemoteAddr(), err)
				_ = WriteError(writer, fmt.Sprintf("protocol error: %v", err))
				_ = writer.Flush()
			} else {
				// Clean disconnect
				log.Printf("Connection closed by %s (EOF)", conn.RemoteAddr())
			}
			return // Close connection on err or EOF
		}

		// 2. Execute command
		response, err := s.executeCommand(cmd)
		if err != nil {
			log.Printf("Error executing command (%s) from %s: %v", cmd.Name(), conn.RemoteAddr(), err)
			_ = WriteError(writer, err.Error()) // Send error response
		} else {
			// 3. Write response
			err = WriteResponse(writer, response)
			if err != nil {
				log.Printf("Error writing response to %s: %v", conn.RemoteAddr(), err)
				return // Close connection if we can't write
			}
		}

		// 4. Flush buffer to ensure data is sent over the network
		err = writer.Flush()
		if err != nil {
			log.Printf("Error flushing writer for %s: %v", conn.RemoteAddr(), err)
			return
		}
	}

}

func (s *Server) executeCommand(cmd *Command) (*Response, error) {
	switch cmd.Type {
	case protocol.CmdSet:
		s.cache.Set(cmd.Key, cmd.Value)
		return &Response{Type: protocol.RespOK}, nil
	case protocol.CmdGet:
		value, found := s.cache.Get(cmd.Key)
		if !found {
			return &Response{Type: protocol.RespNotFound}, nil
		}
		return &Response{Type: protocol.RespValue, Value: value}, nil
	case protocol.CmdDel:
		s.cache.Delete(cmd.Key)
		return &Response{Type: protocol.RespOK}, nil
	default:
		return nil, fmt.Errorf("internal error: unknown command type %d reached execution", cmd.Type)
	}
}
