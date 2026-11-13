package timersync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Watch calls onChange whenever another device's log in dir changes. It polls
// every interval: that needs no dependency and works on every OS and on network
// folders. own is this device's log, which is ignored because its changes are
// already applied locally. It returns when ctx is cancelled.
func Watch(ctx context.Context, dir, own string, interval time.Duration, onChange func()) {
	last := stamp(dir, own)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := stamp(dir, own); cur != last {
				last = cur
				onChange()
			}
		}
	}
}

// stamp summarises the size and mtime of every other device's log.
func stamp(dir, own string) string {
	paths, _ := filepath.Glob(filepath.Join(dir, "*"+Extension))
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		if p == own {
			continue
		}
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d;", p, st.Size(), st.ModTime().UnixNano())
		}
	}
	return b.String()
}
