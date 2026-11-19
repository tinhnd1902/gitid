package tui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// The view of the Online tab.

// ageText is how long ago t was, in one short unit: 5m, 3h, 3d, 4mo, 2y. The
// zero time (the platform does not say) is "–".
func ageText(t, now time.Time) string {
	if t.IsZero() {
		return "–"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "0m"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d/(30*24*time.Hour))) + "mo"
	}
	return strconv.Itoa(int(d/(365*24*time.Hour))) + "y"
}

func (s *reposScreen) onlineView() string {
	st := s.sh.st
	h := maxInt(s.h-1, 3)
	if len(s.oacc) == 0 {
		msg := st.Muted.Render(wrap(tr("tui.repos.online.no_accounts"), maxInt(s.w-6, 10)))
		return s.sh.panel("", msg, s.w, h, true)
	}
	g := s.ogeom()
	var lines []string
	lines = append(lines, s.accountBar(g.iw))
	if s.ofmode != filterOff {
		lines = append(lines, s.onlineFilterLine())
	}
	state := s.onlineStateLines(g.iw)
	head := g.ih - g.listH - g.detail - len(lines)
	lines = append(lines, state[:minInt(len(state), maxInt(head, 0))]...)
	lines = append(lines, s.onlineListLines(g)...)
	if g.detail > 0 {
		lines = append(lines, s.onlineDetailBlock(g)...)
	}
	return s.sh.panel("", strings.Join(lines, "\n"), s.w, h, true)
}

func (s *reposScreen) onlineFilterLine() string {
	st := s.sh.st
	prompt := s.sh.km.keys.Label(keymap.Filter) + " "
	s.ofilter.Prompt = prompt
	if s.ofmode == filterTyping {
		return s.ofilter.View()
	}
	return st.Accent.Render(prompt) + st.Text.Render(s.ofilter.Value()) + st.Faint.Render("  "+tr("tui.repos.online.clear_filter"))
}

// accountBar: ◂ acme/alice · GitHub · token: gh ▸  (1/5)
func (s *reposScreen) accountBar(iw int) string {
	st := s.sh.st
	a := s.oacc[s.osel]
	plat := a.Ref.Account.Platform.DisplayName()
	if h := a.Ref.Account.EffectiveHost(); h != platform.DefaultHost(a.Ref.Account.Platform) {
		plat += " " + h
	}
	info := plat
	if a.Supported {
		src := "…"
		if os := s.sh.online[a.Key]; os != nil && os.phase != opIdle && os.phase != opResolving {
			src = string(os.src)
		}
		info += " · " + tr("tui.repos.online.token", src)
	}
	left, right := "", ""
	if len(s.oacc) > 1 {
		left, right = st.Accent.Render("◂ "), st.Accent.Render(" ▸")+"  "+st.Faint.Render("("+strconv.Itoa(s.osel+1)+"/"+strconv.Itoa(len(s.oacc))+")")
	}
	return trunc(left+st.Bold.Render(a.Ref.String())+st.Muted.Render(" · "+info)+right, iw)
}

// stateLines are the lines under the account bar: what the listing is doing
// or why it has nothing, the hint, the warnings.
func (s *reposScreen) stateLines(os *onlineState, iw int) []string {
	st := s.sh.st
	km := s.sh.km
	a := s.oacc[s.osel]
	// long texts wrap under their mark instead of being cut
	wrapped := func(style lipgloss.Style, mark, t string) []string {
		var out []string
		for i, l := range wrapUnits(t, maxInt(iw-2, 4)) {
			if i == 0 {
				out = append(out, trunc(style.Render(mark+" "+l), iw))
			} else {
				out = append(out, trunc(style.Render("  "+l), iw))
			}
		}
		return out
	}
	warn := func(t string) []string { return wrapped(st.Warn, "!", t) }
	faint := func(t string) []string { return wrapped(st.Faint, " ", t) }
	if !a.Supported {
		return []string{trunc(st.Muted.Render(tr("tui.repos.online.unsupported", a.Ref.Account.Platform.DisplayName())), iw)}
	}
	var out []string
	switch os.phase {
	case opResolving, opIdle:
		out = append(out, trunc(s.spin.View()+" "+st.Muted.Render(tr("tui.repos.online.resolving")), iw))
	case opFetching:
		out = append(out, trunc(s.spin.View()+" "+st.Muted.Render(tr("tui.repos.online.fetching", a.Ref.String(), maxInt(os.pages, 1), groupDigits(len(os.repos)))), iw))
	case opDone:
		switch {
		case len(os.repos) == 0 && os.login == "": // Azure DevOps names no user
			out = append(out, trunc(st.Muted.Render(tr("tui.repos.online.empty_nologin")), iw))
		case len(os.repos) == 0:
			out = append(out, trunc(st.Muted.Render(tr("tui.repos.online.empty", os.login)), iw))
		case os.login == "":
			out = append(out, trunc(st.Faint.Render(tr("tui.repos.online.done_nologin", groupDigits(len(os.repos)))), iw))
		default:
			out = append(out, trunc(st.Faint.Render(tr("tui.repos.online.done", os.login, groupDigits(len(os.repos)))), iw))
		}
	case opCancelled:
		out = append(out, warn(km.T("tui.repos.online.cancelled"))...)
	case opNoToken:
		out = append(out, warn(s.noTokenText(a, os))...)
	case opFailed:
		msg, hint := s.errText(a, os)
		out = append(out, wrapped(st.Err, "✗", msg)...)
		if hint != "" {
			out = append(out, faint(hint)...)
		}
	}
	for _, w := range os.warnings {
		out = append(out, warn(warnText(w))...)
	}
	if os.truncated {
		out = append(out, warn(tr("tui.repos.online.truncated", os.pages, groupDigits(len(os.repos))))...)
	}
	return out
}

func (s *reposScreen) noTokenText(a app.OnlineAccount, os *onlineState) string {
	switch os.note {
	case online.NoteEnvAmbiguous:
		return tr("tui.repos.online.env_ambiguous", a.EnvName)
	case online.NoteInvalidValue:
		return tr("tui.repos.online.invalid_value")
	}
	switch a.Ref.Account.Platform {
	case platform.GitLab:
		return tr("tui.repos.online.no_token.gitlab", a.EnvName)
	case platform.Gitea:
		return tr("tui.repos.online.no_token.gitea", a.EnvName, a.Ref.String())
	case platform.Bitbucket:
		return tr("tui.repos.online.no_token.bitbucket", a.EnvName, a.Ref.String())
	case platform.AzureDevOps:
		return tr("tui.repos.online.no_token.azure", a.EnvName, a.Ref.String())
	}
	return tr("tui.repos.online.no_token.github", a.EnvName)
}

// warnText is the message of a warning of a listing that worked. A warning
// may carry the name it is about after a colon ("org_failed:acme").
func warnText(w string) string {
	code, name := online.WarnParts(w)
	if name == "" {
		return tr("tui.repos.online.warn." + code)
	}
	return tr("tui.repos.online.warn."+code, name)
}

// errText is the message and the hint for a failed listing, in the language
// of the UI. Both are built from fixed texts, the kind of the error and the
// platform's message (already sanitized and redacted by the online package).
func (s *reposScreen) errText(a app.OnlineAccount, os *onlineState) (msg, hint string) {
	km := s.sh.km
	var oe *online.Error
	if !errors.As(os.err, &oe) {
		return tr("tui.repos.online.err.other"), ""
	}
	withDetail := func(m string) string {
		if oe.Detail != "" {
			return m + " (" + oe.Detail + ")"
		}
		return m
	}
	authHint := func() string {
		switch os.src {
		case online.SourceEnv:
			return tr("tui.repos.online.hint.auth.env", a.EnvName)
		case online.SourceKeychain:
			return tr("tui.repos.online.hint.auth.keychain", online.KeychainAccount(a.Account))
		case online.SourceGH:
			return tr("tui.repos.online.hint.auth.gh")
		case online.SourceGlab:
			return tr("tui.repos.online.hint.auth.glab")
		case online.SourceGit:
			if os.pwd {
				return tr("tui.repos.online.hint.auth.git_password", a.EnvName)
			}
			return tr("tui.repos.online.hint.auth.git", a.EnvName)
		}
		return ""
	}
	if a.Ref.Account.Platform == platform.AzureDevOps {
		switch {
		case oe.Kind == online.KindBadResponse && oe.Detail == online.AzureSignInHint:
			return tr("tui.repos.online.err.auth"), tr("tui.repos.online.hint.azure_signin")
		case len(a.Account.Orgs) == 0 && (oe.Kind == online.KindAuth || oe.Kind == online.KindScope || oe.Kind == online.KindBadResponse):
			// the organizations could not be discovered with this token
			return withDetail(tr("tui.repos.online.err.auth")), tr("tui.repos.online.hint.azure_orgs")
		}
	}
	switch oe.Kind {
	case online.KindAuth:
		return withDetail(tr("tui.repos.online.err.auth")), authHint()
	case online.KindOTP:
		return tr("tui.repos.online.err.otp"), tr("tui.repos.online.hint.otp", a.EnvName)
	case online.KindScope:
		k := "tui.repos.online.hint.scope.github"
		switch a.Ref.Account.Platform {
		case platform.GitLab:
			k = "tui.repos.online.hint.scope.gitlab"
		case platform.Gitea:
			k = "tui.repos.online.hint.scope.gitea"
		case platform.Bitbucket:
			k = "tui.repos.online.hint.scope.bitbucket"
		case platform.AzureDevOps:
			k = "tui.repos.online.hint.scope.azure"
		}
		return withDetail(tr("tui.repos.online.err.scope")), tr(k)
	case online.KindSSO:
		if oe.SSOURL != "" {
			return tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso", oe.SSOURL)
		}
		return tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso_nourl")
	case online.KindRateLimit:
		if !oe.Reset.IsZero() {
			return tr("tui.repos.online.err.rate_limit", oe.Reset.In(time.Local).Format("15:04")), ""
		}
		return tr("tui.repos.online.err.rate_unknown"), ""
	case online.KindWrongUser:
		return tr("tui.repos.online.err.wrong_user", oe.Login), tr("tui.repos.online.hint.wrong_user", a.EnvName)
	case online.KindDNS:
		return tr("tui.repos.online.err.dns"), km.T("tui.repos.online.hint.retry")
	case online.KindNetwork:
		return tr("tui.repos.online.err.network"), km.T("tui.repos.online.hint.retry")
	case online.KindTimeout:
		return tr("tui.repos.online.err.timeout"), km.T("tui.repos.online.hint.retry")
	case online.KindServer:
		return withDetail(tr("tui.repos.online.err.server", oe.Status)), km.T("tui.repos.online.hint.retry")
	case online.KindTLS:
		return tr("tui.repos.online.err.tls"), tr("tui.repos.online.hint.tls")
	case online.KindProxy:
		return tr("tui.repos.online.err.proxy"), tr("tui.repos.online.hint.proxy")
	case online.KindBadResponse:
		return withDetail(tr("tui.repos.online.err.bad_response")), tr("tui.repos.online.hint.bad_response")
	case online.KindInsecureHost, online.KindUnsupported:
		return tr("tui.repos.online.err.insecure"), tr("tui.repos.online.hint.bad_response")
	case online.KindCancelled:
		return tr("tui.repos.online.err.cancelled"), ""
	}
	return withDetail(tr("tui.repos.online.err.other")), ""
}

// ---- the list ----

type onlineCols struct{ name, badges, age, also, local int }

// badgeText lists the flags of a repository in one short string.
func badgeText(r online.Repo) string {
	var b []string
	if r.Private {
		b = append(b, tr("tui.repos.online.badge.private"))
	}
	if r.Archived {
		b = append(b, tr("tui.repos.online.badge.archived"))
	}
	if r.Fork {
		b = append(b, tr("tui.repos.online.badge.fork"))
	}
	if r.Empty {
		b = append(b, tr("tui.repos.online.badge.empty"))
	}
	return strings.Join(b, " ")
}

// alsoCount is the number of other accounts whose listing holds the same
// repository.
func (s *reposScreen) alsoCount(key, own string) int {
	if key == "" {
		return 0
	}
	n := 0
	for k, os := range s.sh.online {
		if k == own {
			continue
		}
		if _, ok := os.keyset[key]; ok {
			n++
		}
	}
	return n
}

// localOf is the local clone shown for a repository: the first one, with its
// problem state.
func (s *reposScreen) localOf(key string) (path string, problem, ok bool) {
	clones := s.localClones(key)
	if len(clones) == 0 {
		return "", false, false
	}
	c := clones[0]
	return s.sh.svc.Env.Contract(c.Path), c.Problem(), true
}

func (s *reposScreen) onlineColumns(iw int, os *onlineState) onlineCols {
	var c onlineCols
	wide := iw >= 70
	for _, ri := range s.orows {
		r := os.repos[ri]
		c.name = maxInt(c.name, lipgloss.Width(r.FullPath))
		if iw >= 50 {
			c.badges = maxInt(c.badges, lipgloss.Width(badgeText(r)))
		}
		if wide {
			if n := s.alsoCount(os.keys[ri], os.key); n > 0 {
				c.also = maxInt(c.also, lipgloss.Width(tr("tui.repos.online.also", n)))
			}
			if p, _, ok := s.localOf(os.keys[ri]); ok {
				c.local = maxInt(c.local, lipgloss.Width(p))
			}
		}
	}
	if iw >= 40 {
		c.age = 4
	}
	c.badges = minInt(c.badges, 14)
	c.local = minInt(c.local, maxInt(iw/3, 12))
	size := func() int {
		n := iw - 3
		for _, w := range []int{c.badges, c.age, c.also, c.local} {
			if w > 0 {
				n -= w + 1
			}
		}
		return n
	}
	for _, drop := range []*int{&c.local, &c.also, &c.badges} {
		if size() >= 14 || *drop == 0 {
			continue
		}
		*drop = 0
	}
	c.name = maxInt(size(), 4)
	return c
}

func (s *reposScreen) onlineListLines(g onlineGeom) []string {
	st := s.sh.st
	_, os, ok := s.curAccount()
	var out []string
	if !ok || os == nil {
		for len(out) < g.listH {
			out = append(out, "")
		}
		return out
	}
	if len(s.orows) == 0 && s.ofmode != filterOff && s.onlineTerm() != "" {
		out = append(out, st.Muted.Render(trunc(tr("tui.repos.online.no_match", s.onlineTerm()), g.iw)))
	}
	cols := s.onlineColumns(g.iw, os)
	v := s.curView()
	for i := v.top; i < len(s.orows) && len(out) < g.listH; i++ {
		out = append(out, s.onlineRowLine(i, v.sel, os, cols, g.iw))
	}
	for len(out) < g.listH {
		out = append(out, "")
	}
	return out
}

func (s *reposScreen) onlineRowLine(i, sel int, os *onlineState, cols onlineCols, iw int) string {
	st := s.sh.st
	ri := s.orows[i]
	r, key := os.repos[ri], os.keys[ri]
	path, problem, cloned := s.localOf(key)
	mark, plainMk := " ", " "
	switch {
	case cloned && problem:
		mark, plainMk = st.Warn.Render("!"), "!"
	case cloned:
		mark, plainMk = st.OK.Render("✓"), "✓"
	case r.CloneURL == "":
		mark, plainMk = st.Faint.Render("·"), "·"
	}
	name := pad(r.FullPath, cols.name)
	var parts, plains []string
	add := func(text, styled string) {
		parts = append(parts, " "+styled)
		plains = append(plains, " "+text)
	}
	if cols.badges > 0 {
		b := pad(trunc(badgeText(r), cols.badges), cols.badges)
		add(b, st.Warn.Render(b))
	}
	if cols.age > 0 {
		a := pad(ageText(r.Updated, s.sh.svc.Env.Now()), cols.age)
		add(a, st.Faint.Render(a))
	}
	if cols.also > 0 {
		t := ""
		if n := s.alsoCount(key, os.key); n > 0 {
			t = tr("tui.repos.online.also", n)
		}
		t = pad(t, cols.also)
		add(t, st.Muted.Render(t))
	}
	if cols.local > 0 {
		t := pad(trunc(path, cols.local), cols.local)
		if cloned && problem {
			add(t, st.Warn.Render(t))
		} else {
			add(t, st.Muted.Render(t))
		}
	}
	if i == sel {
		return st.Selected.Render(pad("▌"+plainMk+" "+name+strings.Join(plains, ""), iw))
	}
	return trunc(" "+mark+" "+st.Text.Render(name)+strings.Join(parts, ""), iw)
}

// ---- the detail block ----

func (s *reposScreen) onlineDetailBlock(g onlineGeom) []string {
	st := s.sh.st
	out := []string{st.Faint.Render(strings.Repeat("─", maxInt(g.iw, 4)))}
	d := s.odetail
	if !d.ok {
		out = append(out, st.Faint.Render(trunc(tr("tui.repos.online.none_selected"), g.iw)))
	} else {
		out = append(out, s.onlineDetailLines(d, g.iw, g.detail-1)...)
	}
	for len(out) < g.detail {
		out = append(out, "")
	}
	return out[:g.detail]
}

func (s *reposScreen) onlineDetailLines(d onlineDetail, iw, room int) []string {
	st := s.sh.st
	row := func(label, value string) string {
		return st.Label.Render(pad(label, 11)) + st.Text.Render(trunc(value, maxInt(iw-11, 4)))
	}
	var lines []string
	url := d.url
	if url == "" {
		url = "–"
	}
	cloned := len(d.oc.AlreadyCloned) > 0
	if room >= 4 {
		lines = append(lines, row(tr("tui.f.clone_url"), url))
		switch {
		case cloned:
			lines = append(lines, row(tr("tui.repos.online.d.cloned"), s.sh.svc.Env.Contract(d.oc.AlreadyCloned[0].Path)))
		case d.oc.Plan.Dir != "":
			lines = append(lines, row(tr("tui.f.dir"), s.sh.svc.Env.Contract(d.oc.Plan.Dir)))
		default:
			lines = append(lines, row(tr("tui.f.dir"), "–"))
		}
	}
	text, warn := s.onlineHint(d)
	style := st.Faint
	if warn {
		style = st.Warn
	}
	lines = append(lines, style.Render(trunc(text, iw)))
	return lines
}

// onlineHint is the most useful sentence about the selected repository and
// whether it is a warning.
func (s *reposScreen) onlineHint(d onlineDetail) (string, bool) {
	switch {
	case errors.Is(d.err, app.ErrUnclonable) || d.repo.CloneURL == "":
		return tr("tui.repos.online.d.unclonable"), true
	case errors.Is(d.err, gitops.ErrTargetExists):
		return tr("tui.repos.online.d.taken", s.sh.svc.Env.Contract(errPath(d.err))), true
	case d.err != nil:
		return errText(d.err), true
	case len(d.oc.AlreadyCloned) > 0:
		if c := d.oc.AlreadyCloned[0]; c.Problem() {
			return tr("tui.repos.online.d.problem_hint"), true
		}
		return tr("tui.repos.online.d.check_hint"), false
	}
	if len(d.oc.Warnings) > 0 {
		w := d.oc.Warnings[0]
		if w.Arg == "" {
			return tr(w.Key), true
		}
		return tr(w.Key, w.Arg), true
	}
	return tr("tui.repos.online.d.clone_hint"), false
}

// errPath is the folder in an ErrTargetExists error ("gitops: ...: <path>").
func errPath(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}
