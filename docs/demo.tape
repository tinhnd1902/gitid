# vhs tape for the README hero animation.
#
#   make build
#   vhs docs/demo.tape        # writes docs/img/hero.gif
#
# The tape never touches your own configuration: everything runs against a
# temporary HOME seeded from docs/demo/config.yaml (made-up example data).
# Requires: vhs (https://github.com/charmbracelet/vhs), git, and bin/gitid.

Output docs/img/hero.gif

Require git

Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 720
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms
Set PlaybackSpeed 1

# --- setup (not recorded) ---------------------------------------------------
Hide
Type "REPO=$PWD" Enter
Type "export HOME=$(mktemp -d) GIT_CONFIG_NOSYSTEM=1 GITID_LANG=en" Enter
Type "export GIT_CONFIG_GLOBAL=$HOME/.gitconfig" Enter
Type "mkdir -p $HOME/.config/gitid $HOME/Code/acme/demo && cp $REPO/docs/demo/config.yaml $HOME/.config/gitid/config.yaml" Enter
Type "cd $HOME/Code/acme/demo && git init -q && git remote add origin https://github.com/acme-org/demo.git" Enter
Type "clear" Enter
Show

# --- the tour ---------------------------------------------------------------
Type "$REPO/bin/gitid" Enter
Sleep 2s

# move around the tree
Down 2
Sleep 600ms
Down 3
Sleep 600ms

# fuzzy filter
Type "/"
Sleep 300ms
Type "globex"
Sleep 1500ms
Escape
Sleep 800ms

# preview what would change on disk (nothing is written until you confirm)
Type "p"
Sleep 2500ms
Down 8
Sleep 1000ms
Type "n"
Sleep 800ms

# diagnose the setup
Type "D"
Sleep 2500ms
Escape
Sleep 600ms

# which identity does this repository use?
Type "k"
Sleep 2500ms
Escape
Sleep 600ms

# help overlay, then quit
Type "?"
Sleep 2s
Type "?"
Type "q"
Sleep 1s
