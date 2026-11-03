// SPDX-License-Identifier: AGPL-3.0-or-later

// Package profiles reads and checks game profiles (Design 7, CAT-001,
// CAT-005). A profile describes protocol, ports, command syntax, patterns
// and the command catalog of one game; the catalog is orientation only and
// allows or forbids nothing (CAT-002).
package profiles

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/NightDragon1/rconductor/internal/pattern"
)

// SchemaVersion is the profile schema this release reads.
const SchemaVersion = 1

// Limits of a profile file and its fields.
const (
	MaxFileSize      = 256 << 10
	maxNameLen       = 64
	maxCatalog       = 500
	maxParams        = 8
	maxPathLen       = 256
	maxDescription   = 256
	DefaultMaxLength = 1000
	maxMaxLength     = 10000
)

// Protocols are the adapters of this release (PROT-001); others follow
// with R2 and R3.
var Protocols = []string{"source-rcon"}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Profile is a checked game profile.
type Profile struct {
	Schema     int       `yaml:"schema" json:"-"`
	ID         string    `yaml:"id" json:"-"`
	Name       string    `yaml:"name" json:"-"`
	Match      Match     `yaml:"match" json:"match"`
	Protocol   Protocol  `yaml:"protocol" json:"-"`
	Ports      Ports     `yaml:"ports" json:"ports"`
	Syntax     Syntax    `yaml:"syntax" json:"syntax"`
	Players    *Players  `yaml:"players" json:"players,omitempty"`
	Stop       *Stop     `yaml:"stop" json:"stop,omitempty"`
	Version    *Version  `yaml:"version" json:"version,omitempty"`
	Formatting string    `yaml:"formatting" json:"formatting,omitempty"`
	Catalog    []Command `yaml:"catalog" json:"-"`
}

// Match preselects the profile for container images.
type Match struct {
	Images []string `yaml:"images" json:"images,omitempty"`
}

// Protocol names the adapter and its default port.
type Protocol struct {
	Type        string `yaml:"type"`
	DefaultPort int    `yaml:"default_port"`
}

// Ports lists the game ports (SRV-005).
type Ports struct {
	Game []GamePort `yaml:"game" json:"game"`
}

// GamePort is one game port.
type GamePort struct {
	Port  int    `yaml:"port" json:"port"`
	Proto string `yaml:"proto" json:"proto"`
	Name  string `yaml:"name" json:"name"`
}

// Syntax are the command rules of the game (CAT-005). Missing values get
// the defaults: case-insensitive, leading "/" removed, 1000 characters.
type Syntax struct {
	CaseSensitive *bool    `yaml:"case_sensitive" json:"case_sensitive"`
	StripPrefix   *string  `yaml:"strip_prefix" json:"strip_prefix"`
	MaxLength     int      `yaml:"max_length" json:"max_length"`
	Wrappers      []string `yaml:"wrappers" json:"wrappers"`
	// PlayerPattern is an RE2 pattern a player parameter must match as a
	// whole, e.g. ^[A-Za-z0-9_]{1,16}$ (revision 2.4 candidate 19).
	PlayerPattern string `yaml:"player_pattern" json:"player_pattern,omitempty"`
}

// Players tells how to read the player list.
type Players struct {
	Command   string `yaml:"command" json:"command"`
	Pattern   string `yaml:"pattern" json:"pattern"`
	Separator string `yaml:"separator" json:"separator"`
}

// Stop is the command for a restart without agent (AGT-009).
type Stop struct {
	Command string `yaml:"command" json:"command"`
}

// Version is the log pattern of the game version (AGT-006, R2).
type Version struct {
	Pattern string `yaml:"pattern" json:"pattern"`
}

// Command is a catalog entry (Design 7.3).
type Command struct {
	Path        string  `yaml:"path" json:"path"`
	Description string  `yaml:"description" json:"description"`
	Params      []Param `yaml:"params" json:"params"`
}

// Parameter types (D-041).
const (
	ParamPlayer = "player"
	ParamText   = "text"
	ParamChoice = "choice"
	ParamNumber = "number"
)

// Param is a parameter of a catalog command.
type Param struct {
	Name     string   `yaml:"name" json:"name"`
	Type     string   `yaml:"type" json:"type"`
	Optional bool     `yaml:"optional" json:"optional,omitempty"`
	Values   []string `yaml:"values" json:"values,omitempty"`
	Min      *int     `yaml:"min" json:"min,omitempty"`
	Max      *int     `yaml:"max" json:"max,omitempty"`
}

// Error lists all problems of a rejected profile.
type Error struct{ Problems []string }

func (e *Error) Error() string { return "invalid profile: " + strings.Join(e.Problems, "; ") }

// Parse reads and checks a profile file. Unknown fields are refused, so a
// typo never goes unnoticed; all problems are reported together.
func Parse(data []byte) (*Profile, error) {
	if len(data) > MaxFileSize {
		return nil, &Error{Problems: []string{fmt.Sprintf("file larger than %d bytes", MaxFileSize)}}
	}
	var p Profile
	if err := yaml.NewDecoder(bytes.NewReader(data), yaml.DisallowUnknownField()).Decode(&p); err != nil {
		return nil, &Error{Problems: []string{yaml.FormatError(err, false, false)}}
	}
	if probs := p.check(); len(probs) > 0 {
		return nil, &Error{Problems: probs}
	}
	p.defaults()
	return &p, nil
}

// NormalizePath joins the tokens of a command path with single spaces.
func NormalizePath(path string) string { return strings.Join(strings.Fields(path), " ") }

func (p *Profile) defaults() {
	if p.Syntax.CaseSensitive == nil {
		f := false
		p.Syntax.CaseSensitive = &f
	}
	if p.Syntax.StripPrefix == nil {
		s := "/"
		p.Syntax.StripPrefix = &s
	}
	if p.Syntax.MaxLength == 0 {
		p.Syntax.MaxLength = DefaultMaxLength
	}
	if p.Syntax.Wrappers == nil {
		p.Syntax.Wrappers = []string{}
	}
	if p.Ports.Game == nil {
		p.Ports.Game = []GamePort{}
	}
	for i := range p.Catalog {
		p.Catalog[i].Path = NormalizePath(p.Catalog[i].Path)
		if p.Catalog[i].Params == nil {
			p.Catalog[i].Params = []Param{}
		}
	}
}

func (p *Profile) check() []string {
	var probs []string
	bad := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }

	if p.Schema != SchemaVersion {
		bad("schema must be %d", SchemaVersion)
	}
	if !slugRE.MatchString(p.ID) || len(p.ID) > maxNameLen {
		bad("id is required: lower-case letters, digits and hyphens, at most %d characters", maxNameLen)
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > maxNameLen {
		bad("name is required, at most %d characters", maxNameLen)
	}
	if !slices.Contains(Protocols, p.Protocol.Type) {
		bad("protocol.type must be one of %s", strings.Join(Protocols, ", "))
	}
	if p.Protocol.DefaultPort < 1 || p.Protocol.DefaultPort > 65535 {
		bad("protocol.default_port is required, 1 to 65535")
	}
	for i, gp := range p.Ports.Game {
		if gp.Port < 1 || gp.Port > 65535 || (gp.Proto != "tcp" && gp.Proto != "udp") || strings.TrimSpace(gp.Name) == "" {
			bad("ports.game[%d]: port 1 to 65535, proto tcp or udp and a name", i)
		}
	}
	if m := p.Syntax.MaxLength; m < 0 || m > maxMaxLength {
		bad("syntax.max_length must be 1 to %d", maxMaxLength)
	}
	for i, w := range p.Syntax.Wrappers {
		if NormalizePath(w) == "" || strings.Contains(NormalizePath(w), " ") {
			bad("syntax.wrappers[%d] must be a single command name", i)
		}
	}
	if pp := p.Syntax.PlayerPattern; pp != "" {
		if _, err := regexp.Compile(pp); err != nil || len(pp) > 256 {
			bad("syntax.player_pattern must be an RE2 pattern of at most 256 characters")
		}
	}
	if pl := p.Players; pl != nil {
		if strings.TrimSpace(pl.Command) == "" {
			bad("players.command is required with players")
		}
		if _, err := pattern.Compile(pl.Pattern, 1); err != nil {
			bad("players.pattern: %v", err)
		}
	}
	if p.Stop != nil && strings.TrimSpace(p.Stop.Command) == "" {
		bad("stop.command is required with stop")
	}
	if p.Version != nil {
		if _, err := pattern.Compile(p.Version.Pattern, 1); err != nil {
			bad("version.pattern: %v", err)
		}
	}
	if len(p.Catalog) > maxCatalog {
		bad("at most %d catalog commands", maxCatalog)
	}
	seen := map[string]bool{}
	for i, c := range p.Catalog {
		path := NormalizePath(c.Path)
		switch {
		case path == "" || len(path) > maxPathLen:
			bad("catalog[%d].path is required, at most %d characters", i, maxPathLen)
		case seen[path]:
			bad("catalog[%d].path %q is listed twice", i, path)
		}
		seen[path] = true
		if len(c.Description) > maxDescription {
			bad("catalog[%d].description has at most %d characters", i, maxDescription)
		}
		probs = append(probs, checkParams(i, c.Params)...)
	}
	return probs
}

func checkParams(i int, params []Param) []string {
	var probs []string
	bad := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }
	if len(params) > maxParams {
		bad("catalog[%d]: at most %d params", i, maxParams)
	}
	names := map[string]bool{}
	optional := false
	for j, pa := range params {
		at := fmt.Sprintf("catalog[%d].params[%d]", i, j)
		if !slugRE.MatchString(strings.ReplaceAll(pa.Name, "_", "-")) || names[pa.Name] {
			bad("%s.name is required and unique", at)
		}
		names[pa.Name] = true
		if optional && !pa.Optional {
			bad("%s: a required param cannot follow an optional one", at)
		}
		optional = optional || pa.Optional
		switch pa.Type {
		case ParamPlayer:
		case ParamText:
			if j != len(params)-1 {
				bad("%s: text is allowed only as last param", at)
			}
		case ParamChoice:
			if len(pa.Values) == 0 {
				bad("%s: choice needs values", at)
			}
			for _, v := range pa.Values {
				if v == "" || strings.ContainsAny(v, " \t") {
					bad("%s: choice values are single tokens", at)
				}
			}
		case ParamNumber:
			if pa.Min != nil && pa.Max != nil && *pa.Min > *pa.Max {
				bad("%s: min must not exceed max", at)
			}
		default:
			bad("%s.type must be player, text, choice or number", at)
		}
	}
	return probs
}
