// SPDX-License-Identifier: AGPL-3.0-or-later

package events

import "testing"

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
