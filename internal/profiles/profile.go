// SPDX-License-Identifier: AGPL-3.0-or-later

// Package profiles reads and checks game profiles (Design 7, CAT-001,
// CAT-005). A profile describes protocol, ports, command syntax, patterns
// and the command catalog of one game; the catalog is orientation only and
// allows or forbids nothing (CAT-002).
package profiles

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

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
	// maxAliases and maxDepth are limits of the file format like its size:
	// a profile needs few aliases and little nesting, and a small file must
	// not keep the import busy.
	maxAliases = 100
	maxDepth   = 64
)

// Protocols are the adapters of this release (PROT-001); others follow
// later.
var Protocols = []string{"source-rcon"}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Kinds of a profile (CAT-007): generic for a game as such, specific for
// a modpack or plugin that ranks above it. A profile without kind is
// generic; profiles inherit nothing from each other.
const (
	KindGeneric  = "generic"
	KindSpecific = "specific"
)

// KindOf is the kind of a profile, generic if none is set; for profiles
// stored before the field existed too.
func KindOf(p *Profile) string {
	if p == nil || p.Kind == "" {
		return KindGeneric
	}
	return p.Kind
}

// Profile is a checked game profile.
type Profile struct {
	Schema     int       `yaml:"schema" json:"-"`
	ID         string    `yaml:"id" json:"-"`
	Name       string    `yaml:"name" json:"-"`
	Kind       string    `yaml:"kind" json:"kind,omitempty"`
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

// Ports lists the game ports (SRV-005); SteamConnect offers a
// steam://connect link to a published game port (UI-005, 1.1).
type Ports struct {
	Game         []GamePort `yaml:"game" json:"game"`
	SteamConnect bool       `yaml:"steam_connect" json:"steam_connect,omitempty"`
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
	// Separators end one command and start another in the game's console,
	// e.g. ";" on Source engine servers. A list entry could not see what
	// follows one, so they are refused for moderators and supporters.
	Separators []string `yaml:"separators" json:"separators,omitempty"`
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

// Version is the log pattern of the game version (AGT-006, 1.1).
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
	p, _, err := parse(data)
	return p, err
}

// parse is Parse that also returns the syntax tree, if the file has one, for
// the lines of profile check; the file is parsed once.
func parse(data []byte) (*Profile, *ast.File, error) {
	if len(data) > MaxFileSize {
		return nil, nil, &Error{Problems: []string{fmt.Sprintf("file larger than %d bytes", MaxFileSize)}}
	}
	if prob := checkShape(lexer.Tokenize(string(data))); prob != "" {
		return nil, nil, &Error{Problems: []string{prob}}
	}
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, nil, &Error{Problems: []string{yaml.FormatError(err, false, false)}}
	}
	if prob := checkAliases(file); prob != "" {
		return nil, file, &Error{Problems: []string{prob}}
	}
	var p Profile
	if len(file.Docs) > 0 && file.Docs[0].Body != nil {
		if err := yaml.NodeToValue(file.Docs[0].Body, &p, yaml.DisallowUnknownField()); err != nil {
			return nil, file, &Error{Problems: []string{yaml.FormatError(err, false, false)}}
		}
	}
	if probs := p.check(); len(probs) > 0 {
		return nil, file, &Error{Problems: probs}
	}
	p.defaults()
	return &p, file, nil
}

// at is the position of a token as the YAML errors write it.
func at(tk *token.Token) string { return fmt.Sprintf("[%d:%d]", tk.Position.Line, tk.Position.Column) }

// checkShape refuses too many aliases and too deep nesting on the token
// stream, before anything is built from it. The depth counts
// open flow brackets and the indentation of block keys and list entries.
func checkShape(tokens token.Tokens) string {
	aliases, flow := 0, 0
	var indents []int
	for i, tk := range tokens {
		switch tk.Type {
		case token.AliasType:
			if aliases++; aliases > maxAliases {
				return fmt.Sprintf("%s more than %d aliases", at(tk), maxAliases)
			}
		case token.SequenceStartType, token.MappingStartType:
			flow++
		case token.SequenceEndType, token.MappingEndType:
			flow = max(flow-1, 0)
		}
		key := i+1 < len(tokens) && tokens[i+1].Type == token.MappingValueType
		if flow == 0 && (key || tk.Type == token.SequenceEntryType) {
			for len(indents) > 0 && indents[len(indents)-1] >= tk.Position.Column {
				indents = indents[:len(indents)-1]
			}
			indents = append(indents, tk.Position.Column)
		}
		if len(indents)+flow > maxDepth {
			return fmt.Sprintf("%s nesting deeper than %d levels", at(tk), maxDepth)
		}
	}
	return ""
}

// checkAliases refuses an alias inside an anchored value: anchors that
// repeat other anchors grow exponentially when decoded, even with few
// aliases.
func checkAliases(file *ast.File) string {
	for _, doc := range file.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		for _, n := range ast.Filter(ast.AnchorType, doc.Body) {
			a, ok := n.(*ast.AnchorNode)
			if !ok || a.Value == nil {
				continue
			}
			if inner := ast.Filter(ast.AliasType, a.Value); len(inner) > 0 {
				return fmt.Sprintf("%s an anchored value must not contain aliases", at(inner[0].GetToken()))
			}
		}
	}
	return ""
}

// NormalizePath joins the tokens of a command path with single spaces.
func NormalizePath(path string) string { return strings.Join(strings.Fields(path), " ") }

func (p *Profile) defaults() {
	p.Kind = KindOf(p)
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
	if p.Kind != "" && p.Kind != KindGeneric && p.Kind != KindSpecific {
		bad("kind must be %s or %s", KindGeneric, KindSpecific)
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
		if c.Description == "" || len(c.Description) > maxDescription {
			bad("catalog[%d].description is required, at most %d characters", i, maxDescription)
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
