package keymap

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

// Load reads keys.yaml from the config dir and resolves it. It never fails:
// a missing file gives the defaults and no issue; any other problem gives the
// defaults (all of them, or just the affected actions) plus issues. The file
// is only read, never created or changed.
func Load(e env.Env) (Map, []Issue) {
	path := e.Paths().KeyBindingsFile
	fi, err := os.Stat(path) // follows symlinks, like config.yaml
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Defaults(), nil
		}
		return readIssue(err.Error())
	}
	if !fi.Mode().IsRegular() {
		return readIssue(i18n.T("keys.err.not_regular"))
	}
	if fi.Size() > MaxFileSize {
		return readIssue(i18n.T("keys.err.too_large", fi.Size(), MaxFileSize))
	}
	f, err := os.Open(path)
	if err != nil {
		return readIssue(err.Error())
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return readIssue(err.Error())
	}
	return Parse(data)
}

func readIssue(detail string) (Map, []Issue) {
	return Defaults(), []Issue{{Code: ErrRead, Detail: plain(detail)}}
}

func parseIssue(detail string) (Map, []Issue) {
	return Defaults(), []Issue{{Code: ErrParse, Detail: plain(detail)}}
}

// Parse resolves the content of a keys.yaml. It never fails and never panics.
func Parse(data []byte) (m Map, issues []Issue) {
	defer func() {
		if r := recover(); r != nil {
			m, issues = parseIssue(i18n.T("keys.err.syntax"))
		}
	}()
	if len(data) > MaxFileSize {
		return readIssue(i18n.T("keys.err.too_large", len(data), MaxFileSize))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return parseIssue(syntaxDetail(err))
	}
	return Resolve(&doc)
}

var yamlLineRe = regexp.MustCompile(`line (\d+)`)

// syntaxDetail turns a yaml.v3 error into a localized generic text. The
// library message is English, so only the line number is kept.
func syntaxDetail(err error) string {
	if m := yamlLineRe.FindStringSubmatch(err.Error()); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil {
			return i18n.T("keys.err.syntax_line", n)
		}
	}
	return i18n.T("keys.err.syntax")
}

// deref follows YAML aliases (bounded, so a malformed cycle ends).
func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i < 16; i++ {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" }

type entry struct {
	idx  int      // registry index
	keys []string // normalised, deduplicated
}

// Resolve applies a parsed keys.yaml (the document or its top-level node) to
// the defaults. A nil or empty node gives the defaults.
func Resolve(root *yaml.Node) (Map, []Issue) {
	if root != nil && root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return Defaults(), nil
		}
		root = root.Content[0]
	}
	root = deref(root)
	if root == nil || root.Kind == 0 || isNull(root) {
		return Defaults(), nil
	}
	if root.Kind != yaml.MappingNode {
		return Defaults(), []Issue{{Code: ErrSection}}
	}

	// A repeated name is a YAML mistake the decoder does not report when it
	// reads into a node: treat it as a parse error.
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := deref(root.Content[i])
		if k == nil || k.Kind != yaml.ScalarNode {
			continue
		}
		if seen[k.Value] {
			return parseIssue(i18n.T("keys.err.dup_key", k.Line, fmt.Sprintf("%q", short(k.Value))))
		}
		seen[k.Value] = true
	}

	var issues []Issue
	custom := map[int]entry{}
	var unknown []Issue
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := deref(root.Content[i])
		name := ""
		if k != nil && k.Kind == yaml.ScalarNode {
			name = k.Value
		}
		idx, ok := defIndex[Action(name)]
		if !ok {
			unknown = append(unknown, Issue{Code: ErrUnknownAction, Action: short(name)})
			continue
		}
		def := defs[idx]
		keys, code, bad := parseValue(deref(root.Content[i+1]))
		if code != "" {
			issues = append(issues, Issue{Code: code, Action: name, Key: short(bad), Default: joinKeys(def.Default)})
			continue
		}
		if r, hit := reservedHit(def, keys); hit {
			issues = append(issues, Issue{Code: ErrReserved, Action: name, Key: r.key, Scope: r.scope, Default: joinKeys(def.Default)})
			continue
		}
		if sameKeySet(keys, def.Default) {
			continue // same keys as the default: not a change
		}
		custom[idx] = entry{idx: idx, keys: keys}
	}

	m := Defaults()
	for idx, e := range custom {
		m.keys[defs[idx].Action] = e.keys
		m.custom[defs[idx].Action] = true
	}
	issues = append(issues, resolveConflicts(&m)...)

	// Deterministic order, independent of the order in the file: registry
	// order, then code, then key; unknown names last, sorted by name.
	sort.SliceStable(issues, func(a, b int) bool {
		ia, ib := defIndex[Action(issues[a].Action)], defIndex[Action(issues[b].Action)]
		if ia != ib {
			return ia < ib
		}
		if issues[a].Code != issues[b].Code {
			return issues[a].Code < issues[b].Code
		}
		return issues[a].Key < issues[b].Key
	})
	sort.SliceStable(unknown, func(a, b int) bool { return unknown[a].Action < unknown[b].Action })
	issues = append(issues, unknown...)
	return m, issues
}

// parseValue reads the value of one action: a key or a list of keys. On
// failure it returns the issue code and the offending text (if any).
func parseValue(v *yaml.Node) (keys []string, code, bad string) {
	if v == nil {
		return nil, ErrBadValue, ""
	}
	var raw []string
	switch {
	case isNull(v):
		return nil, ErrBadValue, ""
	case v.Kind == yaml.ScalarNode:
		if v.Value == "" {
			return nil, ErrEmpty, ""
		}
		raw = []string{v.Value}
	case v.Kind == yaml.SequenceNode:
		if len(v.Content) == 0 {
			return nil, ErrEmpty, ""
		}
		for _, it := range v.Content {
			it = deref(it)
			if it == nil || it.Kind != yaml.ScalarNode || isNull(it) {
				return nil, ErrBadValue, ""
			}
			raw = append(raw, it.Value)
		}
	default:
		return nil, ErrBadValue, ""
	}
	for _, r := range raw {
		n, ok := ValidKey(r)
		if !ok {
			return nil, ErrInvalidKey, r
		}
		if !slices.Contains(keys, n) {
			keys = append(keys, n)
		}
	}
	return keys, "", ""
}

// sameKeySet reports whether a and b hold the same keys, in any order.
func sameKeySet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, k := range a {
		if !slices.Contains(b, k) {
			return false
		}
	}
	return true
}

type reservedKey struct {
	key   string
	scope Scope
}

// reservedHit finds a key that a scope of the action uses itself.
func reservedHit(d Def, keys []string) (reservedKey, bool) {
	for _, sc := range scopeOrder {
		if !d.in(sc) {
			continue
		}
		for _, k := range keys {
			if len(k) == 1 && strings.Contains(reserved[sc], k) {
				return reservedKey{k, sc}, true
			}
		}
	}
	return reservedKey{}, false
}

// resolveConflicts reverts custom actions that share a key with another
// action in one scope. It repeats until stable, because a reverted default
// can clash with another custom binding. Defaults never conflict, so every
// pass reverts at least one custom action and the loop ends.
func resolveConflicts(m *Map) []Issue {
	var out []Issue
	for {
		reverted := map[int]Issue{}
		for _, sc := range scopeOrder {
			groups := map[string][]int{}
			for i, d := range defs {
				if !d.in(sc) {
					continue
				}
				for _, k := range m.keys[d.Action] {
					groups[k] = append(groups[k], i)
				}
			}
			ks := make([]string, 0, len(groups))
			for k := range groups {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			for _, k := range ks {
				g := groups[k]
				if len(g) < 2 {
					continue
				}
				for _, i := range g {
					a := defs[i].Action
					if !m.custom[a] {
						continue
					}
					if _, done := reverted[i]; done {
						continue
					}
					other := g[0]
					if other == i {
						other = g[1]
					}
					reverted[i] = Issue{
						Code: ErrConflict, Action: string(a), Key: k, Other: string(defs[other].Action),
						Scope: sc, Default: joinKeys(defs[i].Default),
					}
				}
			}
		}
		if len(reverted) == 0 {
			return out
		}
		idxs := make([]int, 0, len(reverted))
		for i := range reverted {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			a := defs[i].Action
			m.keys[a] = slices.Clone(defs[i].Default)
			delete(m.custom, a)
			out = append(out, reverted[i])
		}
	}
}
