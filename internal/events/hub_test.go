// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package events

import (
	"strings"
	"testing"
)

// TestC3NoDeliveryAfterWithdrawal: an event taken before a subscription ended
// does not go out after it.
func TestC3NoDeliveryAfterWithdrawal(t *testing.T) {
	h := &Hub{}
	s := &subscription{topic: "server.x.logs", server: "x", kind: "logs", ready: true}
	c := &client{out: make(chan []byte, 2), subs: map[string]*subscription{}}
	h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: "secret"})
	if len(c.out) != 0 {
		t.Errorf("sent: %s", <-c.out)
	}
	c.subs[s.topic] = s
	h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: "fine"})
	if len(c.out) != 1 {
		t.Error("a live subscription got nothing")
	}
}

type seqData uint64

func (s seqData) Sequence() uint64 { return uint64(s) }

// TestLogDeliveriesAfterReplay (UI-022): deliveries that waited for the
// masking while a subscriber got the replay of seq 5 go out only when
// newer, each once and in order.
func TestLogDeliveriesAfterReplay(t *testing.T) {
	h := &Hub{}
	s := &subscription{topic: "server.x.logs", server: "x", kind: "logs", ready: true, after: 5}
	c := &client{out: make(chan []byte, 8), subs: map[string]*subscription{}}
	c.subs[s.topic] = s
	for _, q := range []seqData{4, 5, 6, 7} {
		h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: q})
	}
	if len(c.out) != 2 {
		t.Fatalf("sent %d, want 2", len(c.out))
	}
	for _, want := range []string{`"data":6`, `"data":7`} {
		if got := string(<-c.out); !strings.Contains(got, want) {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}
