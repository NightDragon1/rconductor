// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcontest is a Source RCON server for tests: it checks the
// password, answers commands through a handler, splits long answers into
// 4096-byte packets like real servers and answers the end marker either
// like Minecraft or like Source engine servers. In the Minecraft style it
// also drops connections that send several packets at once, as Minecraft
// does. The Factorio style follows a recorded real Factorio server: it
// answers each command in one packet, also an empty one, refuses a packet
// of type 0 without an answer (Factorio logs "RCON received an invalid
// message type 0"; the server counts it) and answers nothing to input
// without a leading "/", which Factorio sends to the chat.
package rcontest

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
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
	// Factorio answers each command in a single packet of any length,
	// sends no empty packet before the auth answer, refuses packets of
	// type 0 (SERVERDATA_RESPONSE_VALUE) without an answer and answers
	// nothing to chat input (no leading "/").
	Factorio
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
	invalid  atomic.Int64
	chat     atomic.Int64
	single   atomic.Bool
	frozen   atomic.Bool
}

// Start listens on a free local port.
func Start(password string, style MarkerStyle, handler func(string) string) (*Server, error) {
	return StartOn("127.0.0.1:0", password, style, handler)
}

// StartOn listens on addr, for a test server in a container.
func StartOn(addr, password string, style MarkerStyle, handler func(string) string) (*Server, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
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

// Invalid counts packets the Factorio style refused as invalid message
// type.
func (s *Server) Invalid() int64 { return s.invalid.Load() }

// SinglePackets makes every style send each answer in one packet of any
// length, as Factorio does, instead of 4096-byte pieces.
func (s *Server) SinglePackets() { s.single.Store(true) }

// Chat counts input the Factorio style took as chat and left unanswered.
func (s *Server) Chat() int64 { return s.chat.Load() }

// Freeze makes the server go silent without closing anything: it still
// reads, but answers nothing, like a host that crashed or lost its
// network behind an open connection (half-open, no FIN or RST).
func (s *Server) Freeze() { s.frozen.Store(true) }

// Thaw ends Freeze.
func (s *Server) Thaw() { s.frozen.Store(false) }

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

// readPacket reads one packet. Like Minecraft, the Minecraft style takes
// one packet per read and drops the connection when a read brings more,
// for example a command and its end marker together.
func (s *Server) readPacket(c net.Conn) ([]byte, bool) {
	if s.Style == Minecraft {
		buf := make([]byte, 4110)
		n, err := c.Read(buf)
		if err != nil || n < 14 || int(binary.LittleEndian.Uint32(buf[:4])) != n-4 {
			return nil, false
		}
		return buf[4:n], true
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return nil, false
	}
	rest := make([]byte, binary.LittleEndian.Uint32(head[:]))
	if _, err := io.ReadFull(c, rest); err != nil {
		return nil, false
	}
	return rest, true
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
		rest, ok := s.readPacket(c)
		if !ok {
			return
		}
		id := int32(binary.LittleEndian.Uint32(rest[0:4]))  //nolint:gosec // wire format
		typ := int32(binary.LittleEndian.Uint32(rest[4:8])) //nolint:gosec // wire format
		body := string(rest[8 : len(rest)-2])
		if s.frozen.Load() {
			continue
		}
		switch {
		case typ == 3: // auth
			if body != s.Password {
				_ = write(c, -1, 2, "")
				return
			}
			authed = true
			if s.Style != Factorio {
				_ = write(c, id, 0, "")
			}
			_ = write(c, id, 2, "")
		case !authed:
			return
		case typ == 2 && s.Style == Factorio && !strings.HasPrefix(body, "/"):
			s.chat.Add(1) // a chat message: no answer at all
		case typ == 2: // command
			s.commands.Add(1)
			answer := body
			if s.Handler != nil {
				answer = s.Handler(body)
			}
			for len(answer) > 4096 && s.Style != Factorio && !s.single.Load() {
				_ = write(c, id, 0, answer[:4096])
				answer = answer[4096:]
			}
			_ = write(c, id, 0, answer)
		case s.Style == Factorio: // refused, logged, never answered
			s.invalid.Add(1)
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
