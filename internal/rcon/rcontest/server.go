// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcontest is a Source RCON server for tests: it checks the
// password, answers commands through a handler, splits long answers into
// 4096-byte packets like real servers and answers the end marker either
// like Minecraft or like Source engine servers.
package rcontest

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// MarkerStyle is how the server answers the empty end-marker packet.
type MarkerStyle int

const (
	// Minecraft answers an unknown packet type with one text packet.
	Minecraft MarkerStyle = iota
	// SourceEngine mirrors the empty packet and sends a second one.
	SourceEngine
)

// Server is a running test server.
type Server struct {
	Addr     string
	Password string
	Style    MarkerStyle
	// Handler answers a command; nil echoes it.
	Handler func(command string) string

	ln       net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	conns    map[net.Conn]bool
	accepted atomic.Int64
	commands atomic.Int64
}

// Start listens on a free local port.
func Start(password string, style MarkerStyle, handler func(string) string) (*Server, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{Addr: ln.Addr().String(), Password: password, Style: style, Handler: handler, ln: ln, conns: map[net.Conn]bool{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Connections counts accepted connections.
func (s *Server) Connections() int64 { return s.accepted.Load() }

// Commands counts executed commands.
func (s *Server) Commands() int64 { return s.commands.Load() }

// DropConnections closes all open connections, as a game server restart
// does.
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
	}
}

// Close stops the server.
func (s *Server) Close() {
	_ = s.ln.Close()
	s.DropConnections()
	s.wg.Wait()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.accepted.Add(1)
		s.mu.Lock()
		s.conns[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(c)
	}
}

func write(w io.Writer, id, typ int32, body string) error {
	buf := binary.LittleEndian.AppendUint32(nil, uint32(10+len(body))) //nolint:gosec // test data
	buf = binary.LittleEndian.AppendUint32(buf, uint32(id))            //nolint:gosec // wire format
	buf = binary.LittleEndian.AppendUint32(buf, uint32(typ))           //nolint:gosec // wire format
	buf = append(buf, body...)
	buf = append(buf, 0, 0)
	_, err := w.Write(buf)
	return err
}

func (s *Server) serve(c net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	authed := false
	for {
		var head [4]byte
		if _, err := io.ReadFull(c, head[:]); err != nil {
			return
		}
		rest := make([]byte, binary.LittleEndian.Uint32(head[:]))
		if _, err := io.ReadFull(c, rest); err != nil {
			return
		}
		id := int32(binary.LittleEndian.Uint32(rest[0:4]))  //nolint:gosec // wire format
		typ := int32(binary.LittleEndian.Uint32(rest[4:8])) //nolint:gosec // wire format
		body := string(rest[8 : len(rest)-2])
		switch {
		case typ == 3: // auth
			if body != s.Password {
				_ = write(c, -1, 2, "")
				return
			}
			authed = true
			_ = write(c, id, 0, "")
			_ = write(c, id, 2, "")
		case !authed:
			return
		case typ == 2: // command
			s.commands.Add(1)
			answer := body
			if s.Handler != nil {
				answer = s.Handler(body)
			}
			for len(answer) > 4096 {
				_ = write(c, id, 0, answer[:4096])
				answer = answer[4096:]
			}
			_ = write(c, id, 0, answer)
		default: // end marker
			if s.Style == Minecraft {
				_ = write(c, id, 0, "Unknown request 0")
			} else {
				_ = write(c, id, 0, "")
				_ = write(c, id, 0, "\x00\x01\x00\x00")
			}
		}
	}
}
