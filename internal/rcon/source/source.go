// SPDX-License-Identifier: AGPL-3.0-or-later

// Package source implements the Source RCON protocol over TCP (PROT-001),
// used by Minecraft, ARK, Factorio and others. Long responses arrive in
// several packets without end mark; after the first packet of a response
// an empty packet is sent as marker, and the response is collected until
// the server answers that marker.
package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
)

// Packet types of the protocol.
const (
	typeResponseValue = 0
	typeExecCommand   = 2
	typeAuthResponse  = 2
	typeAuth          = 3
)

// Limits for foreign data (SEC-006).
const (
	// MaxPacketBody is far above the 4096 bytes servers send; larger
	// packets are refused as broken or hostile.
	MaxPacketBody = 64 << 10
	// MaxResponse bounds the collected response of one command.
	MaxResponse = 1 << 20
	// MaxCommand bounds a command; the profile limit is usually lower.
	MaxCommand = 4000
)

// DefaultTimeout applies when the context has no deadline.
const DefaultTimeout = 10 * time.Second

// Adapter dials Source RCON servers.
type Adapter struct{}

var _ rcon.Adapter = Adapter{}

var (
	_ rcon.Prober = (*Conn)(nil)
	_ rcon.Pinger = (*Conn)(nil)
)

// Conn is an authenticated Source RCON connection.
type Conn struct {
	mu     sync.Mutex
	conn   net.Conn
	r      *bufio.Reader
	nextID int32
}

// KeepAlive is the TCP keepalive of every connection (Design 3.8): the
// kernel notices a dead peer after about 19 s without traffic.
var KeepAlive = net.KeepAliveConfig{Enable: true, Idle: 10 * time.Second, Interval: 3 * time.Second, Count: 3}

// Dial connects and authenticates.
func (Adapter) Dial(ctx context.Context, addr string, password logging.Secret) (rcon.Conn, error) {
	d := net.Dialer{KeepAliveConfig: KeepAlive}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Conn{conn: nc, r: bufio.NewReader(nc), nextID: 1}
	if err := c.auth(ctx, password); err != nil {
		_ = nc.Close()
		return nil, err
	}
	return c, nil
}

func (c *Conn) id() int32 {
	id := c.nextID
	c.nextID++
	if c.nextID <= 0 { // never -1, which means auth failure
		c.nextID = 1
	}
	return id
}

func (c *Conn) deadline(ctx context.Context) {
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(DefaultTimeout)
	}
	_ = c.conn.SetDeadline(dl)
}

func (c *Conn) auth(ctx context.Context, password logging.Secret) error {
	c.deadline(ctx)
	id := c.id()
	if err := c.write(id, typeAuth, password.Reveal()); err != nil {
		return err
	}
	// Some servers send an empty RESPONSE_VALUE before the AUTH_RESPONSE.
	for {
		pid, typ, _, err := c.read()
		if err != nil {
			return err
		}
		if typ != typeAuthResponse {
			continue
		}
		if pid == -1 {
			return rcon.ErrAuth
		}
		if pid != id {
			return fmt.Errorf("rcon auth: unexpected packet id %d", pid)
		}
		return nil
	}
}

// Execute sends command and returns the whole response.
func (c *Conn) Execute(ctx context.Context, command string) (string, error) {
	if command == "" || len(command) > MaxCommand || bytes.IndexByte([]byte(command), 0) >= 0 {
		return "", fmt.Errorf("rcon: invalid command")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline(ctx)
	cmdID, markerID := c.id(), c.id()
	if err := c.write(cmdID, typeExecCommand, command); err != nil {
		return "", err
	}
	var out bytes.Buffer
	marked := false
	for {
		pid, _, body, err := c.read()
		if err != nil {
			return "", err
		}
		switch pid {
		case cmdID:
			if out.Len()+len(body) > MaxResponse {
				return "", rcon.ErrTooLarge
			}
			out.Write(body)
			// The marker goes out only once the command is answered:
			// Minecraft takes one packet per read and drops a connection
			// whose command and marker arrive together. It answers in
			// order, so the marker's answer still comes last.
			if !marked {
				if err := c.write(markerID, typeResponseValue, ""); err != nil {
					return "", err
				}
				marked = true
			}
		case markerID:
			return out.String(), nil
		default:
			// Late answers to earlier markers (Source servers send two)
			// or unrelated packets are dropped.
		}
	}
}

// Ping sends an empty packet, as the end marker, and waits for its answer
// until ctx ends; other packets are dropped (Design 3.8).
func (c *Conn) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline(ctx)
	id := c.id()
	if err := c.write(id, typeResponseValue, ""); err != nil {
		return err
	}
	for {
		pid, _, _, err := c.read()
		if err != nil {
			return err
		}
		if pid == id {
			return nil
		}
	}
}

// Alive reports whether the server still holds the connection, without
// sending anything: a short read that times out means alive, end of
// stream means the server closed it. Between commands every packet that
// arrives is stale, such as a late marker answer, and is dropped.
func (c *Conn) Alive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for range 16 {
		_ = c.conn.SetReadDeadline(time.Now().Add(time.Millisecond))
		_, err := c.r.Peek(1)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil
		}
		if err != nil {
			return err
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, _, _, err := c.read(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the connection.
func (c *Conn) Close() error { return c.conn.Close() }

func (c *Conn) write(id, typ int32, body string) error {
	size := int32(4 + 4 + len(body) + 2) //nolint:gosec // body is bounded by MaxCommand or the password limit
	buf := make([]byte, 0, 4+size)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(size)) //nolint:gosec // positive
	buf = binary.LittleEndian.AppendUint32(buf, uint32(id))   //nolint:gosec // wire format
	buf = binary.LittleEndian.AppendUint32(buf, uint32(typ))  //nolint:gosec // wire format
	buf = append(buf, body...)
	buf = append(buf, 0, 0)
	_, err := c.conn.Write(buf)
	return err
}

func (c *Conn) read() (id, typ int32, body []byte, err error) {
	var head [12]byte
	if _, err = io.ReadFull(c.r, head[:4]); err != nil {
		return 0, 0, nil, err
	}
	size := int32(binary.LittleEndian.Uint32(head[:4])) //nolint:gosec // wire format
	if size < 10 || size > MaxPacketBody+10 {
		return 0, 0, nil, errors.Join(rcon.ErrTooLarge, fmt.Errorf("packet size %d", size))
	}
	rest := make([]byte, size)
	if _, err = io.ReadFull(c.r, rest); err != nil {
		return 0, 0, nil, err
	}
	id = int32(binary.LittleEndian.Uint32(rest[0:4]))  //nolint:gosec // wire format
	typ = int32(binary.LittleEndian.Uint32(rest[4:8])) //nolint:gosec // wire format
	body = bytes.TrimRight(rest[8:], "\x00")
	return id, typ, body, nil
}
