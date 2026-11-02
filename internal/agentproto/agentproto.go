// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentproto is the protocol between panel and agent (Design 1):
// one JSON envelope for all messages over a WebSocket on the agent port.
// Panel and agent share these types; each side treats what the other
// sends as foreign data and checks it here before use (SEC-006, SEC-007).
package agentproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/foreign"
)

// Message types (Design 1.5).
const (
	TypeHello           = "hello"
	TypeWelcome         = "welcome"
	TypeReject          = "reject"
	TypeSnapshot        = "snapshot"
	TypeContainerEvent  = "container.event"
	TypeMetrics         = "metrics"
	TypeContainerAction = "container.action"
	TypeLogsTail        = "logs.tail"
	TypeLogsFollow      = "logs.follow"
	TypeLogsStop        = "logs.stop"
	TypeLogLines        = "log.lines"
	TypeVersionDetect   = "version.detect"
	TypeVolumesSize     = "volumes.size"
	TypeResult          = "result"
	TypeRevoked         = "revoked"
)

// FromAgent and FromPanel list the types each side may send.
var (
	FromAgent = []string{TypeHello, TypeSnapshot, TypeContainerEvent, TypeMetrics, TypeLogLines, TypeResult}
	FromPanel = []string{TypeWelcome, TypeReject, TypeContainerAction, TypeLogsTail, TypeLogsFollow, TypeLogsStop,
		TypeVersionDetect, TypeVolumesSize, TypeRevoked}
)

// Result codes (Design 1.5).
const (
	CodeOK          = "ok"
	CodeForbidden   = "forbidden"
	CodeNotFound    = "not_found"
	CodeInvalid     = "invalid"
	CodeTimeout     = "timeout"
	CodeUnsupported = "unsupported"
	CodeRateLimited = "rate_limited"
)

// Reject reasons (Design 1.3); pending answers a fingerprint pairing that
// waits for the RCON admin (revision 2.3 candidate 4).
const (
	RejectVersion = "version"
	RejectRevoked = "revoked"
	RejectUnknown = "unknown"
	RejectPending = "pending"
	RejectInvalid = "invalid"
)

// Capabilities a release-1 agent reports in hello (Design 1.4).
const (
	CapContainerAction = "container.action"
	CapLogs            = "logs"
	CapMetrics         = "metrics"
)

// Limits (SEC-007, revision 2.3 candidate 2).
const (
	MaxAgentMessage = 1 << 20  // from the agent; above it the panel disconnects
	MaxPanelMessage = 64 << 10 // orders from the panel
	MaxLogLines     = 500
	MaxLogBytes     = 512 << 10
	MaxLogLine      = 8 << 10
	MaxContainers   = 1000
	maxName         = 255
)

// Envelope is the frame of every message: id is unique per message, ref
// names the id of the order an answer belongs to.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Ref     string          `json:"ref,omitempty"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hello opens a session. AgentID is empty before the first pairing; Token
// carries the secret of a pairing token (way A), never logged.
type Hello struct {
	AgentID      string   `json:"agent_id,omitempty"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Backend      string   `json:"backend"`
	Capabilities []string `json:"capabilities"`
	Token        string   `json:"token,omitempty"`
}

// Welcome accepts the agent and names its ID.
type Welcome struct {
	AgentID string `json:"agent_id"`
}

// Reject refuses the agent with a reason.
type Reject struct {
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// Port is a published port of a container.
type Port struct {
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Proto   string `json:"proto"`
}

// Container is a container the label allows.
type Container struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at,omitzero"`
	Ports     []Port    `json:"ports,omitempty"`
	Volumes   []string  `json:"volumes,omitempty"`
}

// Snapshot lists all allowed containers.
type Snapshot struct {
	Containers []Container `json:"containers"`
}

// ContainerEvent is a status change; Gone means the container left the
// set of allowed containers.
type ContainerEvent struct {
	Container Container `json:"container"`
	Gone      bool      `json:"gone,omitempty"`
}

// Metric holds the usage of one container.
type Metric struct {
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemBytes   uint64  `json:"mem_bytes"`
	MemLimit   uint64  `json:"mem_limit"`
	NetRx      uint64  `json:"net_rx"`
	NetTx      uint64  `json:"net_tx"`
	BlkRead    uint64  `json:"blk_read"`
	BlkWrite   uint64  `json:"blk_write"`
}

// Metrics holds the usage of all allowed containers, every 10 s.
type Metrics struct {
	Containers []Metric `json:"containers"`
}

// Actions of container.action.
const (
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
)

// ContainerAction orders start, stop or restart.
type ContainerAction struct {
	Container string `json:"container"`
	Action    string `json:"action"`
}

// LogsTail asks for the last lines of a container.
type LogsTail struct {
	Container string `json:"container"`
	Lines     int    `json:"lines"`
}

// LogsFollow starts or (logs.stop) ends the live logs of a container.
type LogsFollow struct {
	Container string `json:"container"`
}

// LogLines carries log lines of a container.
type LogLines struct {
	Container string   `json:"container"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
}

// Result answers an order.
type Result struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// ErrProtocol marks a message that breaks the protocol.
var ErrProtocol = errors.New("agent protocol violation")

func protoErr(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrProtocol}, args...)...)
}

// New builds an envelope with a fresh id; ref may be empty.
func New(typ, ref string, payload any) (Envelope, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Envelope{}, err
	}
	e := Envelope{Type: typ, ID: id.String(), Ref: ref, TS: time.Now().UTC()}
	if payload != nil {
		if e.Payload, err = json.Marshal(payload); err != nil {
			return Envelope{}, err
		}
	}
	return e, nil
}

// Encode serializes an envelope.
func Encode(typ, ref string, payload any) ([]byte, error) {
	e, err := New(typ, ref, payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// Decode parses one message of at most limit bytes from a sender that may
// send the types in allowed. An unknown type is reported with ok false and
// no error, so the receiver can answer unsupported (Design 1.6).
func Decode(data []byte, limit int, allowed []string) (e Envelope, known bool, err error) {
	if len(data) > limit {
		return e, false, protoErr("message of %d bytes over the limit of %d", len(data), limit)
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, false, protoErr("not a JSON envelope")
	}
	if e.Type == "" || len(e.Type) > 64 || e.ID == "" || len(e.ID) > 64 || len(e.Ref) > 64 {
		return e, false, protoErr("envelope without valid type or id")
	}
	return e, slices.Contains(allowed, e.Type), nil
}

// Payload decodes the payload of e into v.
func Payload[T any](e Envelope) (T, error) {
	var v T
	if len(e.Payload) == 0 {
		return v, protoErr("%s without payload", e.Type)
	}
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		return v, protoErr("%s with invalid payload", e.Type)
	}
	return v, nil
}

// SameMajor reports whether two release versions share the major version
// (AGT-013); unparsable versions never match.
func SameMajor(a, b string) bool {
	ma := buildinfo.MajorOf(a)
	return ma >= 0 && ma == buildinfo.MajorOf(b)
}

// Clean bounds and cleans the foreign data of a hello (SEC-006).
func (h *Hello) Clean() error {
	h.Name = foreign.Name(h.Name, 64)
	h.Version = foreign.Name(h.Version, 64)
	h.Backend = foreign.Name(h.Backend, 32)
	if len(h.Capabilities) > 32 || len(h.AgentID) > 64 || len(h.Token) > 256 {
		return protoErr("hello over its limits")
	}
	for i, c := range h.Capabilities {
		h.Capabilities[i] = foreign.Name(c, 64)
	}
	return nil
}

// Clean bounds and cleans a container from the agent (SEC-006).
func (c *Container) Clean() {
	c.ID = foreign.Name(c.ID, 128)
	c.Name = foreign.Name(c.Name, maxName)
	c.Image = foreign.Name(c.Image, maxName)
	c.State = foreign.Name(c.State, 32)
	if len(c.Ports) > 64 {
		c.Ports = c.Ports[:64]
	}
	if len(c.Volumes) > 64 {
		c.Volumes = c.Volumes[:64]
	}
	for i, v := range c.Volumes {
		c.Volumes[i] = foreign.Name(v, maxName)
	}
}

// LimitLines cleans log lines and bounds them to MaxLogLines lines,
// MaxLogLine bytes per line and MaxLogBytes in all; it keeps the newest
// lines and reports whether anything was cut (revision 2.3 candidate 2).
func LimitLines(lines []string) ([]string, bool) {
	cut := false
	if len(lines) > MaxLogLines {
		lines, cut = lines[len(lines)-MaxLogLines:], true
	}
	out := make([]string, 0, len(lines))
	total := 0
	for i := len(lines) - 1; i >= 0; i-- {
		l, short := foreign.Text(lines[i], MaxLogLine)
		cut = cut || short
		if total+len(l) > MaxLogBytes {
			cut = true
			break
		}
		total += len(l)
		out = append(out, l)
	}
	slices.Reverse(out)
	return out, cut
}
