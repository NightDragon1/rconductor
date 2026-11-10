// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

// Effective merges the profiles of a server, top first, into the rules
// that hold for it (SRV-008, D-074): for each field the topmost profile
// that sets it wins, a field no profile sets keeps its default; wrappers
// are the union of all profiles, no profile lifts one. Protocol, dialect
// and default port come from the top profile; all profiles of a server
// have the same protocol and dialect. The catalog is merged by
// MergeCatalogs. An empty list gives nil.
func Effective(list []*Profile) *Profile {
	if len(list) == 0 {
		return nil
	}
	top := list[0]
	out := &Profile{
		Schema: top.Schema, ID: top.ID, Name: top.Name, Kind: top.Kind, Match: top.Match,
		Protocol: top.Protocol, HTTPAPI: top.HTTPAPI,
	}
	// owner is the topmost profile that sets f, nil if none does.
	owner := func(f string) *Profile {
		for _, p := range list {
			if p != nil && p.setsField(f) {
				return p
			}
		}
		return nil
	}
	if p := owner(FieldCaseSensitive); p != nil {
		out.Syntax.CaseSensitive = p.Syntax.CaseSensitive
	}
	if p := owner(FieldStripPrefix); p != nil {
		out.Syntax.StripPrefix = p.Syntax.StripPrefix
	}
	if p := owner(FieldMaxLength); p != nil {
		out.Syntax.MaxLength = p.Syntax.MaxLength
	}
	if p := owner(FieldPlayerPattern); p != nil {
		out.Syntax.PlayerPattern = p.Syntax.PlayerPattern
	}
	if p := owner(FieldSeparators); p != nil {
		out.Syntax.Separators = append([]string(nil), p.Syntax.Separators...)
	}
	if p := owner(FieldGamePorts); p != nil {
		out.Ports.Game = append([]GamePort(nil), p.Ports.Game...)
	}
	if p := owner(FieldSteamConnect); p != nil {
		out.Ports.SteamConnect = p.Ports.SteamConnect
	}
	if p := owner(FieldPlayers); p != nil {
		out.Players = p.Players
	}
	if p := owner(FieldStop); p != nil {
		out.Stop = p.Stop
	}
	if p := owner(FieldVersion); p != nil {
		out.Version = p.Version
	}
	if p := owner(FieldFormatting); p != nil {
		out.Formatting = p.Formatting
	}
	seen := map[string]bool{}
	for _, p := range list {
		if p == nil {
			continue
		}
		for _, w := range p.Syntax.Wrappers {
			if n := NormalizePath(w); n != "" && !seen[n] {
				seen[n] = true
				out.Syntax.Wrappers = append(out.Syntax.Wrappers, w)
			}
		}
	}
	out.Status = top.Status
	out.defaults()
	out.Set = []string{}
	return out
}

// setsField is Sets, and true for every field of a profile stored before
// the fields were recorded: it then counts as setting what it has.
func (p *Profile) setsField(f string) bool {
	if p.Set == nil {
		return true
	}
	return p.Sets(f)
}

// MergeCatalogs is the catalog of a server: the union of the catalogs of
// its profiles, top first; with the same path the upper profile's entry
// wins (D-074).
func MergeCatalogs(catalogs [][]Command) []Command {
	var out []Command
	seen := map[string]bool{}
	for _, cat := range catalogs {
		for _, c := range cat {
			path := NormalizePath(c.Path)
			if seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, c)
		}
	}
	return out
}
