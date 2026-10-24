package main

import (
	"cmp"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// dirHeader is the number of lines above the first entry of a listing.
const dirHeader = 2

// dirView is a folder shown as a read-only document, one entry per line.
type dirView struct {
	entries []dirEntry // entries[i] is on line dirHeader+i; ".." comes first
	count   int        // entries without ".."
}

type dirEntry struct {
	name string
	path string
	dir  bool
}

const timeFmt = "2006-01-02 15:04"

// listDir builds the listing of a folder: folders first, then files, with
// size, modified and created columns.
func listDir(path string) (*Buffer, *dirView, *lang, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, nil, err
	}
	type row struct {
		dirEntry
		fi os.FileInfo
	}
	rows := make([]row, 0, len(ents))
	for _, ent := range ents {
		p := filepath.Join(path, ent.Name())
		fi, err := ent.Info()
		if err != nil {
			continue // removed while listing
		}
		r := row{dirEntry{name: ent.Name(), path: p, dir: ent.IsDir()}, fi}
		if fi.Mode()&os.ModeSymlink != 0 {
			if t, err := os.Stat(p); err == nil {
				r.dir = t.IsDir()
			}
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b row) int {
		if a.dir != b.dir {
			if a.dir {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})

	v := &dirView{count: len(rows)}
	if parent := filepath.Dir(path); parent != path {
		v.entries = append(v.entries, dirEntry{name: "..", path: parent, dir: true})
	}
	nameW := 20
	for _, r := range rows {
		nameW = max(nameW, utf8.RuneCountInString(r.name)+3)
	}
	nameW = min(nameW, 48)

	var sb strings.Builder
	var total int64
	files := 0
	for _, r := range rows {
		if !r.dir {
			total += r.fi.Size()
			files++
		}
	}
	fmt.Fprintf(&sb, "%s  %d folders · %d files · %s\n", dirName(path), len(rows)-files, files, humanSize(total))
	fmt.Fprintf(&sb, "%-*s %9s  %-16s  %-16s", nameW, "name", "size", "modified", "created")
	if len(v.entries) > 0 {
		sb.WriteString("\n../")
	}
	for _, r := range rows {
		v.entries = append(v.entries, r.dirEntry)
		name := r.name
		if r.dir {
			name += "/"
		} else if r.fi.Mode()&os.ModeSymlink != 0 {
			name += "@"
		}
		if utf8.RuneCountInString(name) > nameW-1 {
			name = string([]rune(name)[:nameW-2]) + "…"
		}
		size := "-"
		if !r.dir {
			size = humanSize(r.fi.Size())
		}
		created, _ := statTimes(r.path, r.fi)
		fmt.Fprintf(&sb, "\n%s%s %9s  %-16s  %s", name, strings.Repeat(" ", nameW-utf8.RuneCountInString(name)),
			size, r.fi.ModTime().Format(timeFmt), fmtTime(created, timeFmt))
	}
	return NewBuffer(sb.String()), v, &lang{dirCols: nameW}, nil
}

func dirName(path string) string {
	p := shortPath(path)
	if !strings.HasSuffix(p, string(filepath.Separator)) {
		p += string(filepath.Separator)
	}
	return p
}

func fmtTime(t time.Time, layout string) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "-"
	}
	return t.Format(layout)
}

func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, units := float64(n)/1024, "KMGTP"
	for f >= 1024 && len(units) > 1 {
		f, units = f/1024, units[1:]
	}
	return fmt.Sprintf("%.1f %cB", f, units[0])
}

// scanDir colors a listing: the path in the header, folder names, and dimmed
// columns. The state counts the header lines.
func (l *lang) scanDir(line string, st int, out []span) ([]span, int) {
	switch st {
	case 0:
		k := strings.Index(line, "  ")
		if k < 0 {
			k = len(line)
		}
		return append(out, span{0, k, tokKeyword}, span{k, len(line), tokComment}), 1
	case 1:
		return append(out, span{0, len(line), tokComment}), 2
	}
	// Byte offset of the end of the name column.
	end, n := 0, 0
	for end < len(line) && n < l.dirCols {
		_, w := utf8.DecodeRuneInString(line[end:])
		end += w
		n++
	}
	name := strings.TrimRight(line[:end], " ")
	switch {
	case strings.HasSuffix(name, "/"):
		out = append(out, span{0, len(name), tokTag})
	case strings.HasPrefix(name, "."):
		out = append(out, span{0, len(name), tokComment})
	}
	return append(out, span{end, len(line), tokComment}), 2
}

// ---- file properties ----

type prop struct{ key, val string }

// fileProps describes a file or folder on disk. lines is the line count of
// the open document, or -1.
func fileProps(path string, lines int, unsaved bool) []prop {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []prop{{"", "not saved to disk yet"}}
		}
		return []prop{{"", err.Error()}}
	}
	ps := []prop{{"kind", kindOf(fi)}}
	if fi.Mode()&os.ModeSymlink != 0 {
		t, _ := os.Readlink(path)
		ps = append(ps, prop{"link to", t})
	}
	switch {
	case fi.IsDir():
		if ents, err := os.ReadDir(path); err == nil {
			ps = append(ps, prop{"items", strconv.Itoa(len(ents))})
		}
	default:
		ps = append(ps, prop{"size", fmt.Sprintf("%s (%s bytes)", humanSize(fi.Size()), groupDigits(fi.Size()))})
	}
	if lines >= 0 {
		ps = append(ps, prop{"lines", groupDigits(int64(lines))})
	}
	created, accessed := statTimes(path, fi)
	const full = "2006-01-02 15:04:05"
	ps = append(ps,
		prop{"created", fmtTime(created, full) + ago(created)},
		prop{"modified", fmtTime(fi.ModTime(), full) + ago(fi.ModTime())},
		prop{"accessed", fmtTime(accessed, full) + ago(accessed)},
		prop{"mode", fmt.Sprintf("%s (%04o)", fi.Mode().Perm(), fi.Mode().Perm())},
	)
	if o := statOwner(fi); o != "" {
		ps = append(ps, prop{"owner", o})
	}
	if unsaved {
		ps = append(ps, prop{"", "has unsaved changes"})
	}
	return ps
}

func kindOf(fi os.FileInfo) string {
	m := fi.Mode()
	switch {
	case m.IsDir():
		return "folder"
	case m&os.ModeSymlink != 0:
		return "symlink"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeDevice != 0:
		return "device"
	case m&0o111 != 0:
		return "executable file"
	}
	if ext := filepath.Ext(fi.Name()); ext != "" {
		return strings.TrimPrefix(ext, ".") + " file"
	}
	return "file"
}

// ago gives a rough age like " (3 min ago)" for recent times.
func ago(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return " (just now)"
	case d < time.Hour:
		return fmt.Sprintf(" (%d min ago)", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf(" (%d h ago)", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf(" (%d days ago)", int(d.Hours()/24))
	}
	return ""
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

var owners sync.Map // "uid:gid" -> "user:group"

func ownerName(uid, gid uint32) string {
	key := fmt.Sprint(uid, ":", gid)
	if v, ok := owners.Load(key); ok {
		return v.(string)
	}
	u, g := strconv.Itoa(int(uid)), strconv.Itoa(int(gid))
	if x, err := user.LookupId(u); err == nil {
		u = x.Username
	}
	if x, err := user.LookupGroupId(g); err == nil {
		g = x.Name
	}
	owners.Store(key, u+":"+g)
	return u + ":" + g
}
