// Package keymap is the registry of the rebindable keyboard shortcuts and
// the loader of the optional user file keys.yaml.
//
// The package is pure: it does not import the TUI library. Both the TUI and
// the `gitid keys` and `gitid doctor` commands use it. gitid only ever reads
// keys.yaml; it never creates, rewrites, backs up or deletes it.
//
// The names in the registry are part of the file format. They are stable and
// never renamed (a new version may add names; an older version reports the
// unknown ones as a warning and ignores them).
package keymap

import (
	"slices"
	"strings"
)

// Action is the stable, snake_case name of a rebindable shortcut.
type Action string

// Actions.
const (
	Quit       Action = "quit"
	Help       Action = "help"
	Back       Action = "back"
	Filter     Action = "filter"
	AddAccount Action = "add_account"
	AddCompany Action = "add_company"
	Edit       Action = "edit"
	Delete     Action = "delete"
	Clone      Action = "clone"
	Check      Action = "check"
	Doctor     Action = "doctor"
	Apply      Action = "apply"
	SSHKey     Action = "ssh_key"
	Import     Action = "import"
	Reload     Action = "reload"
	Yes        Action = "yes"
	No         Action = "no"
	Fix        Action = "fix"
	Retry      Action = "retry"
	Copy       Action = "copy"
	Open       Action = "open"
	SSHTest    Action = "ssh_test"
	Terminal   Action = "terminal"
)

// Scope is a set of keys that are live at the same time on one screen. A
// modal dialog that replaces the key set is its own scope. Two actions that
// share a scope can not share a key.
type Scope string

const (
	ScopeList      Scope = "list"
	ScopeDetail    Scope = "detail"
	ScopeApply     Scope = "apply"
	ScopeApplyDone Scope = "apply_done"
	ScopeCheck     Scope = "check"
	ScopeDialog    Scope = "dialog"
	ScopeClone     Scope = "clone"
	ScopeKeyResult Scope = "key_result"
	ScopeDoctor    Scope = "doctor"
	ScopeImport    Scope = "import"
	ScopeForm      Scope = "form"
)

const (
	maxKeyFileBytes = 64 << 10
	maxEchoRunes    = 24
	maxDetailRunes  = 120
	// Letters handled by the dialogs and by the form itself.
	reservedInDialog = "hl"
	reservedInForm   = "yYnNhljk"
)

// MaxFileSize is the largest keys.yaml that is read.
const MaxFileSize = maxKeyFileBytes

var scopeOrder = []Scope{
	ScopeList, ScopeDetail, ScopeApply, ScopeApplyDone, ScopeCheck, ScopeDialog,
	ScopeClone, ScopeKeyResult, ScopeDoctor, ScopeImport, ScopeForm,
}

// reserved are the letters a scope uses itself (they are handled inside
// dialogs and forms and can not be rebound for an action living there).
var reserved = map[Scope]string{
	ScopeDialog: reservedInDialog,
	ScopeForm:   reservedInForm,
}

// Def describes one rebindable action.
type Def struct {
	Action  Action
	Default []string
	Scopes  []Scope
	DescKey string // i18n key of the description
}

func (d Def) in(s Scope) bool { return slices.Contains(d.Scopes, s) }

var allScopesButForm = []Scope{ScopeDetail, ScopeApply, ScopeApplyDone, ScopeCheck, ScopeDialog, ScopeClone, ScopeKeyResult, ScopeDoctor, ScopeImport}

func scopes(s ...Scope) []Scope { return s }

// defs is the registry, in display order.
var defs = []Def{
	{Quit, []string{"q"}, scopes(ScopeList), "tui.key.quit"},
	{Help, []string{"?"}, append(scopes(ScopeList), allScopesButForm...), "tui.key.help"},
	{Back, []string{"q"}, append(slices.Clone(allScopesButForm), ScopeForm), "tui.key.back"},
	{Filter, []string{"/"}, scopes(ScopeList), "tui.key.filter"},
	{AddAccount, []string{"a"}, scopes(ScopeList, ScopeDetail), "tui.key.add_account"},
	{AddCompany, []string{"A"}, scopes(ScopeList), "tui.key.add_company"},
	{Edit, []string{"e"}, scopes(ScopeList, ScopeDetail), "tui.key.edit"},
	{Delete, []string{"d"}, scopes(ScopeList, ScopeDetail), "tui.key.delete"},
	{Clone, []string{"c"}, scopes(ScopeList, ScopeDetail, ScopeApplyDone, ScopeCheck), "tui.key.clone"},
	{Check, []string{"k"}, scopes(ScopeList, ScopeDetail, ScopeApplyDone), "tui.key.check"},
	{Doctor, []string{"D"}, scopes(ScopeList, ScopeDetail), "tui.key.doctor"},
	{Apply, []string{"p"}, scopes(ScopeList, ScopeDetail, ScopeKeyResult), "tui.key.apply"},
	{SSHKey, []string{"g"}, scopes(ScopeList, ScopeDetail), "tui.key.genkey"},
	{Import, []string{"i"}, scopes(ScopeList), "tui.key.import"},
	{Reload, []string{"r"}, scopes(ScopeList), "tui.key.reload"},
	{Yes, []string{"y"}, scopes(ScopeApply, ScopeDialog), "tui.key.yes"},
	{No, []string{"n"}, scopes(ScopeApply, ScopeDialog), "tui.key.no"},
	{Fix, []string{"f"}, scopes(ScopeCheck), "tui.key.fix"},
	{Retry, []string{"r"}, scopes(ScopeApply, ScopeCheck, ScopeDoctor), "tui.key.retry"},
	{Copy, []string{"c"}, scopes(ScopeKeyResult), "tui.key.copy"},
	{Open, []string{"o"}, scopes(ScopeKeyResult), "tui.key.open"},
	{SSHTest, []string{"s"}, scopes(ScopeDoctor), "tui.key.ssh_test"},
	{Terminal, []string{"t"}, scopes(ScopeClone), "tui.key.terminal"},
}

var defIndex = func() map[Action]int {
	m := make(map[Action]int, len(defs))
	for i, d := range defs {
		m[d.Action] = i
	}
	return m
}()

// Defs returns the registry in display order (a copy).
func Defs() []Def {
	out := make([]Def, len(defs))
	for i, d := range defs {
		d.Default = slices.Clone(d.Default)
		d.Scopes = slices.Clone(d.Scopes)
		out[i] = d
	}
	return out
}

// Scopes returns every scope in the order conflicts are reported.
func Scopes() []Scope { return slices.Clone(scopeOrder) }

// Reserved returns the letters a scope handles itself ("" when none).
func Reserved(s Scope) string { return reserved[s] }

// Map is the effective key bindings: the defaults with the valid custom
// entries of keys.yaml applied. The zero value behaves like Defaults().
type Map struct {
	keys   map[Action][]string
	custom map[Action]bool
}

// Defaults returns the built-in bindings.
func Defaults() Map {
	m := Map{keys: make(map[Action][]string, len(defs)), custom: map[Action]bool{}}
	for _, d := range defs {
		m.keys[d.Action] = slices.Clone(d.Default)
	}
	return m
}

// Keys returns the keys bound to a (a copy; nil for an unknown action).
func (m Map) Keys(a Action) []string {
	if k, ok := m.keys[a]; ok {
		return slices.Clone(k)
	}
	if i, ok := defIndex[a]; ok && m.keys == nil {
		return slices.Clone(defs[i].Default)
	}
	return nil
}

// Label is the first key of an action, used in help bars and hints.
func (m Map) Label(a Action) string {
	if k := m.Keys(a); len(k) > 0 {
		return k[0]
	}
	return ""
}

// Custom reports whether keys.yaml changed the binding of a.
func (m Map) Custom(a Action) bool { return m.custom[a] }

// CustomCount is the number of actions changed by keys.yaml.
func (m Map) CustomCount() int { return len(m.custom) }

// DefaultKeys returns the built-in keys of an action.
func DefaultKeys(a Action) []string {
	if i, ok := defIndex[a]; ok {
		return slices.Clone(defs[i].Default)
	}
	return nil
}

// Known reports whether name is a registered action.
func Known(name string) bool { _, ok := defIndex[Action(name)]; return ok }

func joinKeys(k []string) string { return strings.Join(k, ", ") }
