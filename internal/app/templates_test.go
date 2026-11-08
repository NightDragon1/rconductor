// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestTemplatesThroughTheStack covers CMD-008 over HTTP: missing and
// oversized fields are refused; a template goes with its server the same
// soft way; parallel creates with one name give one template.
func TestTemplatesThroughTheStack(t *testing.T) {
	f := newRightsFixture(t)
	admin := f.as(rA)
	path := "/servers/" + f.server + "/templates"
	for _, body := range []any{
		map[string]string{"name": "n"},
		map[string]string{"template": "kick {player}"},
		map[string]string{"name": "n", "template": strings.Repeat("x", 5000)},
		map[string]string{"name": strings.Repeat("n", 65), "template": "kick {player}"},
		map[string]string{"name": "n", "template": "{cmd} x"},
	} {
		if st := admin.call(http.MethodPost, path, body, nil); st != http.StatusBadRequest && st != http.StatusUnprocessableEntity {
			t.Errorf("%.40v: %d", body, st)
		}
	}

	// Parallel creates with the same name: exactly one wins.
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for range 8 {
		wg.Go(func() {
			if admin.call(http.MethodPost, path, map[string]string{"name": "Same", "template": "say {text}"}, nil) == http.StatusCreated {
				mu.Lock()
				created++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if created != 1 {
		t.Errorf("created %d templates with one name", created)
	}

	// A deleted server takes its templates along, softly.
	srv := f.freshServer()
	var tmpl struct{ ID string }
	admin.must(http.MethodPost, "/servers/"+srv+"/templates", map[string]string{"name": "k", "template": "kick {player}"}, &tmpl, http.StatusCreated)
	admin.must(http.MethodDelete, "/servers/"+srv, nil, nil, http.StatusNoContent)
	var deleted int
	if err := f.db().QueryRow(`SELECT count(*) FROM command_templates WHERE id = ? AND deleted_at IS NOT NULL`, tmpl.ID).Scan(&deleted); err != nil || deleted != 1 {
		t.Errorf("template after the server's delete: %d %v", deleted, err)
	}
	if st := admin.call(http.MethodGet, "/servers/"+srv+"/templates", nil, nil); st != http.StatusNotFound {
		t.Errorf("templates of a deleted server = %d", st)
	}
}
