package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/keys"
)

type keygenPhase int

const (
	kgMode keygenPhase = iota
	kgNoPassConfirm
	kgRunning
	kgFinishing
	kgError
)

// keygenScreen generates an ed25519 key. ssh-keygen runs attached to the
// terminal through tea.ExecProcess, so it asks for the passphrase itself and
// gitid never sees it. Creating a key without a passphrase needs a second,
// explicit confirmation.
type keygenScreen struct {
	sh     *shared
	ref    config.AccountRef
	id     int
	phase  keygenPhase
	mode   choice // 0 = with passphrase, 1 = without
	sure   choice // second confirmation, default no
	errMsg string
	spin   spinner.Model
	w, h   int
}

func newKeygenScreen(sh *shared, ref config.AccountRef) *keygenScreen {
	return &keygenScreen{
		sh: sh, ref: ref, id: sh.id(), spin: sh.st.Spinner,
		mode: choice{labels: [2]string{tr("tui.keygen.with_pass"), tr("tui.keygen.no_pass")}},
		sure: choice{labels: [2]string{tr("tui.keygen.back"), tr("tui.keygen.confirm_nopass")}, danger: true},
	}
}

func (k *keygenScreen) Title() string   { return tr("tui.keygen.title") }
func (k *keygenScreen) Init() tea.Cmd   { return k.spin.Tick }
func (k *keygenScreen) Resize(w, h int) { k.w, k.h = w, h }
func (k *keygenScreen) Capturing() bool { return k.phase == kgRunning || k.phase == kgFinishing }

// StepBack: esc/q in the no-passphrase confirmation returns to the mode choice.
func (k *keygenScreen) StepBack() bool {
	if k.phase == kgNoPassConfirm {
		k.phase = kgMode
		return true
	}
	return false
}

func (k *keygenScreen) Help() []key.Binding {
	km := k.sh.km
	switch k.phase {
	case kgMode, kgNoPassConfirm:
		return []key.Binding{km.Toggle, km.Enter, km.Back}
	case kgError:
		return []key.Binding{km.Back}
	}
	return nil
}

func (k *keygenScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := k.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if k.phase == kgFinishing {
			var cmd tea.Cmd
			k.spin, cmd = k.spin.Update(msg)
			return k, cmd
		}
		return k, nil
	case keyExecDoneMsg:
		if msg.id != k.id {
			return k, nil
		}
		if msg.err != nil {
			k.phase, k.errMsg = kgError, errText(msg.err)
			return k, nil
		}
		k.phase = kgFinishing
		return k, tea.Batch(k.spin.Tick, k.finish())
	case keyDoneMsg:
		if msg.id != k.id {
			return k, nil
		}
		if msg.err != nil {
			k.phase, k.errMsg = kgError, errText(msg.err)
			if errors.Is(msg.err, app.ErrConfigChanged) {
				return k, changed(msg.cfg) // show what is on disk now
			}
			return k, nil
		}
		pub := msg.pub
		return k, tea.Batch(changed(msg.cfg), say(levelOK, tr("tui.status.key_created")), replace(newKeyResultScreen(k.sh, k.ref, &pub)))
	case tea.KeyMsg:
		switch k.phase {
		case kgMode:
			switch {
			case key.Matches(msg, km.Toggle):
				k.mode.toggle()
			case key.Matches(msg, km.Enter):
				if k.mode.idx == 1 {
					k.phase = kgNoPassConfirm
					k.sure.idx = 0
					return k, nil
				}
				return k, k.run(false)
			}
		case kgNoPassConfirm:
			switch {
			case key.Matches(msg, km.Toggle):
				k.sure.toggle()
			case key.Matches(msg, km.Yes):
				return k, k.run(true)
			case key.Matches(msg, km.No):
				k.phase = kgMode
			case key.Matches(msg, km.Enter):
				if k.sure.idx == 1 {
					return k, k.run(true)
				}
				k.phase = kgMode
			}
		}
	}
	return k, nil
}

// run starts ssh-keygen attached to the terminal.
func (k *keygenScreen) run(noPass bool) tea.Cmd {
	cmd, _, err := k.sh.svc.KeygenExec(k.ref, noPass)
	if err != nil {
		k.phase, k.errMsg = kgError, errText(err)
		return nil
	}
	k.phase = kgRunning
	id := k.id
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return keyExecDoneMsg{id: id, err: err} })
}

// finish verifies the new key, records it in the configuration and reads the
// public key (never the private one).
func (k *keygenScreen) finish() tea.Cmd {
	svc, cfg, ref, id := k.sh.svc, k.sh.cfg, k.ref, k.id
	return func() tea.Msg {
		// Never overwrite what another gitid window saved in the meantime.
		if disk, stale, err := svc.ChangedSince(cfg); err != nil {
			return keyDoneMsg{id: id, err: err}
		} else if stale {
			return keyDoneMsg{id: id, cfg: disk, err: app.ErrConfigChanged}
		}
		pub, saved, err := svc.FinishKey(cfg, ref)
		return keyDoneMsg{id: id, pub: pub, cfg: saved, err: err}
	}
}

func (k *keygenScreen) View() string {
	st := k.sh.st
	inner := maxInt(k.w-6, 10)
	var b strings.Builder
	b.WriteString(st.Bold.Render(k.ref.String()) + "  " + st.Muted.Render(k.sh.svc.Env.Contract(keyPath(k.sh, k.ref))) + "\n\n")
	switch k.phase {
	case kgMode:
		b.WriteString(wrap(st.Muted.Render(tr("tui.keygen.explain")), inner) + "\n\n")
		b.WriteString(k.mode.view(st) + "\n")
	case kgNoPassConfirm:
		b.WriteString(st.Warn.Render("! "+wrap(tr("tui.keygen.nopass_warn"), inner)) + "\n\n")
		b.WriteString(k.sure.view(st) + "\n")
	case kgRunning:
		b.WriteString(st.Muted.Render(tr("tui.keygen.running")))
	case kgFinishing:
		b.WriteString(k.spin.View() + " " + st.Muted.Render(tr("tui.keygen.finishing")))
	case kgError:
		b.WriteString(st.Err.Render("✗ " + wrap(k.errMsg, inner)))
	}
	return k.sh.panel(tr("tui.keygen.title"), b.String(), k.w, k.h, true)
}

// keyPath is the private key path used for an account (display only).
func keyPath(sh *shared, ref config.AccountRef) string {
	if ref.Account.SSHKey != "" {
		return sh.svc.Env.Expand(ref.Account.SSHKey)
	}
	return sh.svc.Env.Expand(ref.Company.DefaultKeyPath(ref.Account))
}

// keyResultScreen shows the public key of an account with its "add key" page.
type keyResultScreen struct {
	sh     *shared
	ref    config.AccountRef
	id     int
	pub    *keys.Pub
	errMsg string
	spin   spinner.Model
	w, h   int
}

func newKeyResultScreen(sh *shared, ref config.AccountRef, pub *keys.Pub) *keyResultScreen {
	return &keyResultScreen{sh: sh, ref: ref, pub: pub, id: sh.id(), spin: sh.st.Spinner}
}

func (k *keyResultScreen) Title() string   { return tr("tui.keyresult.title") }
func (k *keyResultScreen) Resize(w, h int) { k.w, k.h = w, h }

func (k *keyResultScreen) Init() tea.Cmd {
	if k.pub != nil {
		return nil
	}
	svc, ref, id := k.sh.svc, k.ref, k.id
	return tea.Batch(k.spin.Tick, func() tea.Msg {
		pub, err := svc.ExistingKey(ref)
		return keyLoadedMsg{id: id, pub: pub, err: err}
	})
}

func (k *keyResultScreen) Help() []key.Binding {
	km := k.sh.km
	out := []key.Binding{km.Copy, km.Open}
	if k.sh.pending > 0 {
		out = append(out, km.Apply)
	}
	return append(out, km.Back)
}

func (k *keyResultScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := k.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if k.pub == nil && k.errMsg == "" {
			var cmd tea.Cmd
			k.spin, cmd = k.spin.Update(msg)
			return k, cmd
		}
	case keyLoadedMsg:
		if msg.id != k.id {
			return k, nil
		}
		if msg.err != nil {
			k.errMsg = errText(msg.err)
			return k, nil
		}
		p := msg.pub
		k.pub = &p
	case copyDoneMsg:
		if msg.id != k.id {
			return k, nil
		}
		if msg.err != nil {
			return k, say(levelWarn, tr("tui.keyresult.copy_failed"))
		}
		return k, say(levelOK, tr("tui.keyresult.copied"))
	case openDoneMsg:
		if msg.id != k.id {
			return k, nil
		}
		if msg.err != nil {
			return k, say(levelWarn, tr("tui.keyresult.open_failed"))
		}
		return k, say(levelInfo, tr("tui.keyresult.opened"))
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, km.Copy) && k.pub != nil:
			svc, content, id := k.sh.svc, k.pub.Content, k.id
			return k, func() tea.Msg {
				ctx, cancel := ctxTimeout(shortTimeout)
				defer cancel()
				return copyDoneMsg{id: id, err: svc.CopyKey(ctx, content)}
			}
		case key.Matches(msg, km.Open):
			svc, acc, id := k.sh.svc, k.ref.Account, k.id
			return k, func() tea.Msg {
				ctx, cancel := ctxTimeout(shortTimeout)
				defer cancel()
				return openDoneMsg{id: id, err: svc.OpenKeyURL(ctx, acc)}
			}
		case key.Matches(msg, km.Apply) && k.sh.pending > 0:
			return k, k.sh.pushNow(newConfirmScreen(k.sh, tr("tui.confirm.title_apply")))
		}
	}
	return k, nil
}

func (k *keyResultScreen) View() string {
	st := k.sh.st
	inner := maxInt(k.w-6, 10)
	var b strings.Builder
	b.WriteString(st.Bold.Render(k.ref.String()) + "  " + st.Muted.Render(k.sh.svc.Env.Contract(keyPath(k.sh, k.ref))) + "\n\n")
	switch {
	case k.errMsg != "":
		b.WriteString(st.Err.Render("✗ " + wrap(k.errMsg, inner)))
	case k.pub == nil:
		b.WriteString(k.spin.View() + " " + st.Muted.Render(tr("tui.keyresult.loading")))
	default:
		b.WriteString(st.Label.Render(tr("tui.keyresult.pub")) + "\n")
		b.WriteString(st.Accent.Render(ansi.Hardwrap(k.pub.Content, inner, true)) + "\n\n")
		if url := keys.KeyURL(k.ref.Account); url != "" {
			b.WriteString(st.Label.Render(tr("tui.keyresult.add_at")) + "\n" + st.Info.Render(trunc(url, inner)) + "\n\n")
		}
		if k.sh.pending > 0 {
			b.WriteString(st.Warn.Render("! " + wrap(k.sh.km.T("tui.keyresult.apply_hint"), inner)))
		}
	}
	return k.sh.panel(tr("tui.keyresult.title"), b.String(), k.w, k.h, true)
}
