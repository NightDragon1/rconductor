// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"net/http"
	"slices"
	"testing"
)

// D-106: deleting a user deletes their groups and command lists.
// Members of such a group lose it. Groups and lists of other users stay.
func TestD106DeleteUserDeletesGroupsAndLists(t *testing.T) {
	a := admin(t)

	// A server admin with a moderator group and a command list, and a moderator as member.
	sa, saID := invite(t, a, "server_admin", "acct-sa-106", "Acct-Server-Admin-106!")
	var list struct {
		ID string `json:"id"`
	}
	sa.do(http.MethodPost, "/command-lists", map[string]any{
		"name":    "acct-list-106",
		"entries": []map[string]any{{"path": "kick", "passable": false}},
	}).want(t, "command list of the server admin", http.StatusCreated).decode(t, &list)
	var group struct {
		ID string `json:"id"`
	}
	sa.do(http.MethodPost, "/user-groups", map[string]any{
		"name": "acct-group-106", "kind": "moderator", "command_list_id": list.ID,
	}).want(t, "group of the server admin", http.StatusCreated).decode(t, &group)
	_, modID := invite(t, sa, "moderator", "acct-mod-106", "Acct-Moderator-106!")
	sa.do(http.MethodPut, "/users/"+modID+"/assignments", map[string]any{"groups": []string{group.ID}}).
		want(t, "moderator into the group", http.StatusOK)

	// Control: a group and a list of the admin itself.
	var ctlList, ctlGroup struct {
		ID string `json:"id"`
	}
	a.do(http.MethodPost, "/command-lists", map[string]any{
		"name":    "acct-list-106-admin",
		"entries": []map[string]any{{"path": "kick", "passable": false}},
	}).want(t, "control list", http.StatusCreated).decode(t, &ctlList)
	a.do(http.MethodPost, "/user-groups", map[string]any{"name": "acct-group-106-admin", "kind": "general"}).
		want(t, "control group", http.StatusCreated).decode(t, &ctlGroup)

	// Before: the admin sees both, the moderator is a member.
	if !slices.Contains(groupIDs(t, a), group.ID) || !slices.Contains(listIDs(t, a), list.ID) {
		t.Fatal("before the deletion the admin does not see the group or list of the server admin")
	}
	if !slices.Contains(memberGroups(t, a, modID), group.ID) {
		t.Fatal("before the deletion the moderator is not a member of the group")
	}

	a.do(http.MethodDelete, "/users/"+saID, nil).want(t, "delete the server admin", http.StatusNoContent)

	if slices.Contains(groupIDs(t, a), group.ID) {
		t.Error("group of the deleted server admin still exists")
	}
	if slices.Contains(listIDs(t, a), list.ID) {
		t.Error("command list of the deleted server admin still exists")
	}
	if slices.Contains(memberGroups(t, a, modID), group.ID) {
		t.Error("the moderator is still a member of the deleted group")
	}
	if !slices.Contains(groupIDs(t, a), ctlGroup.ID) || !slices.Contains(listIDs(t, a), ctlList.ID) {
		t.Error("group or list of another user was deleted too")
	}
}

func groupIDs(t *testing.T, c *client) []string {
	t.Helper()
	var l struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	c.do(http.MethodGet, "/user-groups", nil).want(t, "GET /user-groups", http.StatusOK).decode(t, &l)
	var ids []string
	for _, g := range l.Items {
		ids = append(ids, g.ID)
	}
	return ids
}

func listIDs(t *testing.T, c *client) []string {
	t.Helper()
	var l struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	c.do(http.MethodGet, "/command-lists", nil).want(t, "GET /command-lists", http.StatusOK).decode(t, &l)
	var ids []string
	for _, g := range l.Items {
		ids = append(ids, g.ID)
	}
	return ids
}

func memberGroups(t *testing.T, c *client, userID string) []string {
	t.Helper()
	var as struct {
		Groups []string `json:"groups"`
	}
	c.do(http.MethodGet, "/users/"+userID+"/assignments", nil).want(t, "GET assignments", http.StatusOK).decode(t, &as)
	return as.Groups
}
