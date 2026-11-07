// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestB11_1StaleRowHidesHostChange (blind round B11, finding 1): Update read
// the row outside the transaction and wrote every column from that read. A
// rename sent with a predicted version that read the row before a
// concurrent move committed wrote the old host back, while its audit named
// only "name". Now the row is read in the transaction after the version
// check, so the last audited host is always the stored one. Fails on
// a416454.
func TestB11_1StaleRowHidesHostChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	admin := f.as("admin")
	for i := range 50 { // failed in the first round on a416454
		sv := f.create(fmt.Sprintf("s%d", i))
		evil, safe := "evil.example", "mc.lan"
		if _, err := f.svc.Update(ctx, admin, sv.ID, Input{Host: &evil, Version: f.version(sv.ID)}); err != nil {
			t.Fatal(err)
		}
		v := *f.version(sv.ID)
		next := v + 2 // an update raised the version by two on a416454
		name := fmt.Sprintf("renamed%d", i)
		var wg sync.WaitGroup
		var errA, errB error
		wg.Add(2)
		go func() { defer wg.Done(); _, errA = f.svc.Update(ctx, admin, sv.ID, Input{Host: &safe, Version: &v}) }()
		go func() {
			defer wg.Done()
			for range 200 { // too early is a harmless conflict
				if _, errB = f.svc.Update(ctx, admin, sv.ID, Input{Name: &name, Version: &next}); errB == nil {
					return
				}
			}
		}()
		wg.Wait()
		if errA != nil || errB != nil {
			continue
		}
		var host string
		if err := f.st.DB.QueryRowContext(ctx, `SELECT host FROM servers WHERE id = ?`, sv.ID).Scan(&host); err != nil {
			t.Fatal(err)
		}
		lastTo := lastAuditedHost(t, f, sv.ID)
		if host != lastTo {
			t.Fatalf("iteration %d: server points to %q, last audited host is %q (rename hid the host change)", i, host, lastTo)
		}
	}
}

// lastAuditedHost is the host of the newest server.update entry that
// names a host change.
func lastAuditedHost(t *testing.T, f *fixture, id string) string {
	t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(), `SELECT json_extract(payload, '$.host.to') FROM audit_log
		WHERE action = 'server.update' AND target_id = ? AND json_extract(payload, '$.host.to') IS NOT NULL ORDER BY rowid`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var last string
	for rows.Next() {
		if err := rows.Scan(&last); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return last
}
