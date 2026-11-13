// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"cmp"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"
)

// SupportedGamesPage is the wiki page Supported games from the profiles in
// fsys (DOC-005, CAT-001): one row per profile, sorted by name, with the
// origin fields of D-072. scripts/wiki-supported-games.sh prints it; a test
// holds it against the shipped profiles, so the page cannot drift from
// profiles/. A file that is no valid profile is an error, not a gap.
func SupportedGamesPage(fsys fs.FS) (string, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return "", err
	}
	list := make([]*Profile, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return "", err
		}
		p, err := Parse(data)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		list = append(list, p)
	}
	slices.SortFunc(list, func(a, b *Profile) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})

	var b strings.Builder
	b.WriteString(`The panel ships a game profile for each game below. A profile is
orientation only: it fills the command catalog, the player list and the
fields of the command page, and it allows and forbids nothing. Rights come
from roles and command lists alone.

- **Status** ` + "`tested`" + ` means a run with a real server confirmed the profile;
  ` + "`untested`" + ` means it was researched from the sources only.
- **Researched version** is the game version the commands were researched
  for; newer versions may differ.
- **Kind** ` + "`generic`" + ` is a game as such, ` + "`specific`" + ` a modpack or plugin
  that ranks above it.

Your own profiles in ` + "`/data/profiles/*.yaml`" + ` replace a shipped one with the
same id. A profile is missing or wrong? Open an issue with the form
"Game profile: new game or correction":
https://github.com/NightDragon1/rconductor/issues/new/choose

This page is generated from ` + "`profiles/`" + ` by ` + "`scripts/wiki-supported-games.sh`" + `.

| Game | Id | Kind | Protocol | Researched version | Status | Sources |
| --- | --- | --- | --- | --- | --- | --- |
`)
	for _, p := range list {
		version := "unknown"
		if p.ResearchedVersion != "" {
			version = cell(p.ResearchedVersion)
		}
		sources := make([]string, 0, len(p.Sources))
		for _, s := range p.Sources {
			sources = append(sources, sourceLink(s))
		}
		src := strings.Join(sources, ", ")
		if src == "" {
			src = "none"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | `%s` | %s | %s | %s |\n",
			cell(p.Name), cell(p.ID), cell(p.Kind), cell(p.Protocol.Type), version, cell(p.Status), src)
	}
	return b.String(), nil
}

// cell makes a value safe for one Markdown table cell: a pipe would end
// the cell, a line break the row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}

// sourceLink is one source as a link: the first word, if it is an http(s)
// URL, becomes the link and the rest stays as a note behind it; any other
// source stays text.
func sourceLink(s string) string {
	link, note, _ := strings.Cut(strings.TrimSpace(s), " ")
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return cell(s)
	}
	out := "[" + strings.ReplaceAll(cell(linkText(u)), "]", `\]`) + "](<" + cell(link) + ">)"
	if note = cell(note); note != "" {
		out += " " + note
	}
	return out
}

// linkText is the short name of a source URL: host and last path element.
func linkText(u *url.URL) string {
	last := path.Base(strings.TrimSuffix(u.Path, "/"))
	if last == "." || last == "/" || last == "" {
		return u.Host
	}
	if unescaped, err := url.PathUnescape(last); err == nil {
		last = unescaped
	}
	return u.Host + ": " + last
}
