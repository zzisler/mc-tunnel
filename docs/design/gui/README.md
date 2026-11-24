# GUI redesign notes (`dev/gui`)

Goal: make the window look like a modern dark app instead of a stock Win32
form, and add a light/dark switch. The tunnel, proxy and settings packages do
not change either way: `ui.Run()` is the only entry point the rest of the
program knows about.

## Mockups

`mock.py` renders the target look with Pillow, so the palette and geometry can
be tweaked and re-rendered while the real drawing is rebuilt:

```
python docs/design/gui/mock.py docs/design/gui/mock-expanded.png expanded
python docs/design/gui/mock.py docs/design/gui/mock-collapsed.png collapsed
```

`mock-expanded.png` — 930x391 client area, form column plus log panel.
`mock-collapsed.png` — 430x391, log hidden behind the chip in the header.

## Rendering options, measured on this machine

Checked with Go 1.25.7 (`CGO_ENABLED=0`), Windows 11.

| Option | Builds here | Exe size | Notes |
| --- | --- | --- | --- |
| Current Win32 (`internal/ui`) | yes | 5.27 MB | window appears in ~200-270 ms, no dependencies |
| [Gio](https://gioui.org) v0.10.3 | yes, pure Go | 6.20 MB stripped | hello window appeared in 230-270 ms |
| [Wails](https://wails.io) v2.17.1 | Go side yes, frontend needs npm | 10.77 MB | needs the WebView2 runtime (157.0.4341.60 is installed) |
| [Fyne](https://fyne.io) v2.8.3-rc1 | no | — | needs a C compiler |

Details:

- **Fyne** pulls `github.com/go-gl/gl` through `internal/driver/glfw`, which is
  cgo. With `CGO_ENABLED=0` the build stops at
  `imports github.com/go-gl/gl/v2.1/gl: build constraints exclude all Go files`.
  There is no gcc/cc/clang on this machine and no MSYS2 or mingw tree, so Fyne
  would first need a toolchain install. It also renders through OpenGL.
- **Gio** compiles and runs with no C toolchain and no runtime dependencies.
  Everything is drawn by the program itself, which suits the mockup: rounded
  rectangles, custom segmented control and coloured log lines are primitives.
  Text editing and scrolling come from `widget.Editor` / `widget.List`.
- **Wails** compiles without cgo and produces a single exe. Two caveats found
  here: the frontend build runs npm, and npm cannot write its cache inside this
  sandbox; and at runtime WebView2 creates its environment but then fails to
  create the webview controller, because Chromium's helper processes are cut
  off by the sandbox. Both are harness limitations, not Wails bugs — but it
  means a Wails UI cannot be exercised from this session, only by running the
  exe by hand.

## Verification limits

Windows created by the harness shell cannot be screenshotted: the harness
browser window is always on top and raising another process's window is
blocked across integrity levels. So visual checks of the real app need a human
at the screen; the mockups above are the design reference in the meantime.

## Decision

Not made yet. Leaning towards Gio: it reproduces the mockup closely, keeps the
zero-dependency single-exe property, and can be run and checked from here.
