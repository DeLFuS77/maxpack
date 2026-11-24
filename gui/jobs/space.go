// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// Drive is what a queue asks of one drive: its root, the most its games may take there (each one
// at most its original size, as a .p5z or a folder is never bigger) and what is free on it.
type Drive struct {
	Root       string
	Need, Free uint64
}

// Space adds up, by drive, what the plans that go may take, and gives the drives where that is
// more than is free: the queue may not fit there, and the user is told before anything starts.
// Each game alone is warned of in its plan (may_not_fit); a game that replaces another needs both
// at once, which its plan already tells (no_space).
func Space(plans []Plan, o Options, fs FS) []Drive {
	short := []Drive{}
	if o.Mode == ModeVerify || o.Dest == "" {
		return short
	}
	need := map[string]uint64{}
	var roots []string
	for _, p := range plans {
		if p.Block != "" || p.Out == "" {
			continue
		}
		root := fs.Root(o.Dest)
		if _, seen := need[root]; !seen {
			roots = append(roots, root)
		}
		need[root] += p.Item.OriginalSize
	}
	for _, root := range roots {
		free, err := fs.Free(o.Dest)
		if err == nil && need[root] > free {
			short = append(short, Drive{Root: root, Need: need[root], Free: free})
		}
	}
	return short
}
