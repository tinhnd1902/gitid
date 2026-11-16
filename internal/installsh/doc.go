// Package installsh holds the tests for install.sh at the repository root.
//
// The tests run the real script with sh (and dash / bash --posix when
// present) against a local httptest server that imitates the GitHub release
// URLs. Every run uses temporary HOME, TMPDIR, INSTALL_DIR and system bin
// directories, a fake uname, and a fully constructed environment, so nothing
// outside the temporary directories is touched and no network is used.
package installsh
