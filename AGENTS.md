# Working on nib

nib is a text editor for files of any size, written in Go on
[Gio](https://gioui.org), in one `package main`. Its reason to exist is
that it stays fast and small when the file does not: a 2 GB file opens in
under a second and the editor stays under ~300 MB. Every change is judged
against that first. The numbers in the README's "How it is fast" are the
bar, not a historical note.

## The performance rules

These are the invariants the design rests on. A change that breaks one is
wrong even if it is small, tidy and well tested.

### 1. A frame only does on-screen work

`Editor.Layout` runs on every frame and is allowed to cost O(what is
visible), never O(file).

- `drawText` walks lines `first..last` of the viewport and nothing else.
  Highlighting (`highlight.go`) scans only those lines, and skips lines
  longer than `maxHighlight` (10 KB).
- A long line (over `longLine`, 4 KB) is never walked from its start: the
  `colIndex` in `text.go` remembers where every 4 KB falls on screen, and
  only the visible part of it is drawn or searched for matches.
- Anything per frame that touches every line, every block or every byte
  is a bug, no matter how cheap each step is. A 22-million-line file makes
  any per-line loop in `Layout` a dropped frame.

If you need a count or a position that depends on the whole file, compute
it in the background and `poll` for it, or derive it from the block index
in O(log blocks).

### 2. Lines live in blocks of 1,024

`lines.go` keeps the document as blocks of `blockLines` (1,024) lines. A
block fresh from disk holds only 4-byte line-start offsets into the
memory-mapped file; it becomes real strings only when edited. That is
what keeps memory flat and an edit at ~30 µs in any file.

- Do not add per-line state. A slice with one entry per line costs 176 MB
  in a 22-million-line file before it holds anything useful. State goes
  per block, per screen or per edit.
- An edit touches one block. Keep it that way: no renumbering, no
  reindexing beyond the block, no copying the file.
- An edit across several lines merges the blocks it spans and splits
  anything over 2,048 lines back into 1,024-line blocks, so blocks stay
  small and lookups stay O(log blocks).

### 3. Nothing that scales with the file on the main thread

Work proportional to the file runs somewhere the frame does not wait for it:

- Indexing a file of `asyncMin` (8 MB) or more happens in the background;
  `Buffer.Poll` takes the blocks in as they arrive, so the first screen is
  up before the index is done.
- The folder walk, the file finder and the folder search (`finder.go`,
  `grep.go`) run in goroutines and report through `poll`.
- Operations the user asks for on the whole file (find, replace all,
  count, save) run on all cores through `parallelFor`, and unedited blocks
  are processed as one slice of the file rather than line by line. Counts
  have a time budget (`countMatches`) and report partial results.
- `parallelFor` returns false when a worker faulted on a memory-mapped
  file that was truncated under it. Callers must then leave every state
  untouched; a half-applied operation on a vanished file is worse than
  none.

When adding a feature, decide where its file-sized work runs before
writing it. If the answer is "in the key handler" or "in `Layout`", it is
the wrong answer.

## Checking your work

```sh
go vet ./... && go test ./...
```

### Benchmarks

The benchmarks that matter need a big file in `NIB_FILE`; without it they
skip. A real one is best (a log, an XML dump, a minified bundle). For a
synthetic one:

```sh
NIB_GEN=/tmp/big.log NIB_GEN_MB=256 go test -run TestGenLog .
NIB_FILE=/tmp/big.log go test -run '^$' -bench . -count 6 . | tee after.txt
```

Run that before and after a change that touches `buffer.go`, `lines.go`,
`search.go`, `text.go`, `highlight.go`, `finder.go` or `grep.go`, and
compare with `benchstat before.txt after.txt`. The benchmarks are
`IndexBig`, `LoadBig`, `TypeInBig`, `FindBig`, `ReplaceAllBig`,
`SaveBig` (the big file), `Scan` (highlighting), `VisColLong` (a 1 MB
line) and `Rank`/`Walk` (the finder, on `NIB_ROOT`).

CI runs the same benchmarks on every pull request, for the PR and for its
merge base, six rounds each taking turns on the same runner, and fails the
PR when `benchstat` finds a benchmark significantly more than 10% slower
(`bench` job in `.github/workflows/ci.yml`, judged by
`scripts/benchcheck`). If it fails, make the change faster; it is not
a flaky check to rerun until green. If a slowdown is the deliberate price
of a feature, say so in the PR with the numbers, and the threshold or the
benchmark can be changed in the same PR, visibly.

### Seeing what you changed

The editor can draw itself offscreen, so you can look at a UI change
without a display. This needs a GPU; the tests skip without one.

```sh
NIB_SHOT=out.png NIB_FILE=main.go go test -run TestRender .
```

Then open `out.png` (an agent can read the image). `TestRender` takes
these settings:

| Variable | Shows |
| --- | --- |
| `NIB_FILE=path` | that file or folder (default `editor.go`); `-` is an empty scratch buffer |
| `NIB_LINE=line[:col]` | the cursor there |
| `NIB_TABS=a.go,b.go` | more files open, the first one unsaved; `NIB_TABX=1` hovers the first tab's × |
| `NIB_FINDER=query` | the file finder |
| `NIB_PALETTE=query` | the command palette |
| `NIB_GREP=query` | the folder search |
| `NIB_FIND=text` / `NIB_REPLACE=old:new` | the find or replace prompt |
| `NIB_KEYS=1` / `NIB_INFO=1` | the key reference (`NIB_KEYS=end` scrolled to its end) / file properties |
| `NIB_LIGHT=1` / `NIB_WS=1` | the light theme / whitespace shown |

For a change that should not move pixels it does not mean to, use the
golden images. Record baselines before the change, make it, and run again:

```sh
NIB_GOLDEN=/tmp/golden go test -run TestGolden .   # first run: writes the baselines
# ... change the code ...
NIB_GOLDEN=/tmp/golden go test -run TestGolden .   # fails on any scene that changed
```

A failing scene writes `<scene>.diff.png` beside the baseline with the
changed pixels in red, and the failure names the rectangle they fall in.
Look at it: if that is the change you meant, rerun with
`NIB_GOLDEN_UPDATE=1`; if not, you found a side effect. The scenes are
listed in `goldenScenes` in `render_test.go` and render a generated file,
so editing nib's own sources does not disturb them. Baselines are per
machine (fonts and GPUs rasterize differently) and are not committed.

`NIB_GIF=docs/demo.gif go test -run TestGif .` records the README demo
and `NIB_ICON=appicon.png go test -run TestIcon .` redraws the icon from
the strokes in `logo.go`.

## Conventions

- One package, one file per concern (`tabs.go`, `search.go`, ...), tests
  beside it in `_test.go`. New code goes in the file whose header comment
  describes it, or a new file with such a header.
- Comments are sentences that say why, in the voice of the existing ones.
  A constant gets its reason (`asyncMin = 8 << 20 // files at least this
  big are indexed in the background`).
- No new dependencies without a reason that survives the question "could
  this be 50 lines here?".
- Changes to the keys go in `keyRows` (`panels.go`) and the README's key
  table together.
