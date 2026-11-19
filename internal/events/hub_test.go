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

// TestPublishMessageOnlyToParticipants (D-085): an event of the topic
// messages reaches the sockets of the given users only, never a text.
func TestPublishMessageOnlyToParticipants(t *testing.T) {
	h := &Hub{clients: map[*client]bool{}}
	sockets := map[string]*client{}
	for _, user := range []string{"sender", "recipient", "other"} {
		c := &client{out: make(chan []byte, 2), subs: map[string]*subscription{}}
		c.p.UserID = user
		c.subs["messages"] = &subscription{topic: "messages", kind: "messages", ready: true}
		h.clients[c] = true
		sockets[user] = c
	}
	h.PublishMessage([]string{"sender", "recipient"}, MessageEvent{Kind: "comment", MessageID: "m1", CommentID: "c1"})
	for user, want := range map[string]int{"sender": 1, "recipient": 1, "other": 0} {
		if got := len(sockets[user].out); got != want {
			t.Errorf("%s got %d events, want %d", user, got, want)
		}
	}
	if got := string(<-sockets["recipient"].out); got != `{"type":"event","topic":"messages","data":{"kind":"comment","message_id":"m1","comment_id":"c1"}}` {
		t.Errorf("event = %s", got)
	}
}
