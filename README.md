# quod

A small, fast, GPU-rendered text editor written in Go with [Gio](https://gioui.org).
Runs on macOS, Linux and Windows.

- Only the lines on screen are drawn: a frame takes under 0.1 ms.
- Large files are memory-mapped and indexed in the background, so the first
  screen shows up right away: ~2 ms for 100 MB, ~20 ms for 2 GB.
- Lines live in blocks of offsets into the file (4 bytes per line until edited),
  so an edit touches one small block: ~30 µs even in a 22-million-line file.

Text is set in [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (NL, no ligatures),
embedded in the binary under the OFL (see `fonts/OFL.txt`).

```sh
go install github.com/hipjim/quod@latest
quod file.txt
# or from a clone:
go build -o quod . && ./quod file.txt
```

*quod* comes from **quod** erat demonstrandum, in tribute to
[QED](https://en.wikipedia.org/wiki/QED_(text_editor)) (1965), the editor that
led to `ed`, `vi` and everything after.

## Keys

`⌘` is Ctrl on Linux and Windows; word moves use Alt on macOS and Ctrl elsewhere.

| Key | Action |
| --- | --- |
| `⌘S` / `⌘⇧S` | save / save as |
| `⌘O` | open (Tab completes paths) |
| `⌘F`, then `Enter` / `⇧Enter` / `F3` | find next / previous (lower-case query = case-insensitive) |
| `⌘G` | go to `line[:col]` |
| `⌘Z` / `⌘⇧Z` / `⌘Y` | undo / redo |
| `⌘C` / `⌘X` / `⌘V` | copy / cut / paste (no selection = whole line) |
| `⌘D` | select word, then next occurrence |
| `⌘L` | select line (repeat to extend) |
| `⌘⇧D` / `⌘⇧K` | duplicate / delete line |
| `Alt↑` / `Alt↓` | move line up / down |
| `Tab` / `⇧Tab` | indent / dedent (works on selections) |
| `⌘=` / `⌘-` / `⌘0` | zoom in / out / reset |
| `Esc` | clear selection, close prompt |

Double-click selects a word, triple-click a line.

## Tests

```sh
go test ./...
QUOD_SHOT=out.png go test -run TestRender .   # offscreen GPU screenshot
```

## License

MIT. The bundled JetBrains Mono font is under the SIL Open Font License (`fonts/OFL.txt`).
