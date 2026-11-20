package update

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
)

// maxCacheBytes is the largest cache file that is read.
const maxCacheBytes = 4 << 10

type cacheState int

const (
	cacheStale   cacheState = iota // missing, unreadable, old or not trusted: ask again
	cacheFresh                     // young enough: no request
	cacheBlocked                   // not a regular file: no request, no write
)

// cacheFile is the content of update-check.json.
type cacheFile struct {
	Checked string `json:"checked"`
	Latest  string `json:"latest"`
}

// cacheData is the cache after validation.
type cacheData struct {
	// latest is the last valid answer (zero: none).
	latest [3]int
}

// readCache reads the cache at path. The text of the file is never kept: only
// a version that passed ParseRelease survives, so a hand-edited or hostile
// file can not put anything on the screen.
func readCache(path string, now time.Time) (cacheData, cacheState) {
	var out cacheData
	fi, err := os.Lstat(path)
	if err != nil {
		return out, cacheStale // missing (or a parent that is not a directory)
	}
	if !fi.Mode().IsRegular() {
		return out, cacheBlocked // a directory, a link, a FIFO, a device
	}
	if fi.Size() > maxCacheBytes {
		return out, cacheStale
	}
	f, err := os.Open(path)
	if err != nil {
		return out, cacheStale
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil || len(data) > maxCacheBytes {
		return out, cacheStale
	}
	var cf cacheFile
	if json.Unmarshal(data, &cf) != nil {
		return out, cacheStale
	}
	checked, err := time.Parse(time.RFC3339, cf.Checked)
	if err != nil {
		return out, cacheStale
	}
	if cf.Latest != "" {
		v, ok := ParseRelease(cf.Latest)
		if !ok {
			return out, cacheStale // not trusted at all
		}
		out.latest = v
	}
	age := now.Sub(checked)
	if age < -futureSlack || age >= Interval {
		return out, cacheStale
	}
	return out, cacheFresh
}

// writeCache stamps the attempt. It only writes where gitid's config folder
// already exists (the check never creates it) and ignores every error.
func writeCache(e env.Env, path string, now time.Time, latest [3]int) {
	if fi, err := os.Stat(e.Paths().ConfigDir); err != nil || !fi.IsDir() {
		return
	}
	cf := cacheFile{Checked: now.UTC().Format(time.RFC3339)}
	if latest != ([3]int{}) {
		cf.Latest = format(latest)
	}
	data, err := json.Marshal(cf)
	if err != nil {
		return
	}
	_ = fsx.WriteAtomic(path, append(data, '\n'), 0o600)
}
