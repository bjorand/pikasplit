# pikasplit

Control a Mac from a window on a Windows PC, over the local network, so you no
longer have to switch the monitor's input.

- `pikasplit-host` runs on the Mac: it creates a virtual display sized to the
  viewer window, receives keyboard and mouse input and injects it there.
- `pikasplit-view` runs on the PC: while its window has focus, the PC's
  keyboard and mouse go to the Mac.

Status: milestone 3 (keyboard/mouse + virtual display). The Mac's video in the
window comes in later milestones; until then, look at the virtual display from
the Mac side (e.g. System Settings > Displays, or mirror it).

## Build

On the Mac (Xcode Command Line Tools + Go):

```sh
go build -o bin/pikasplit-host ./cmd/pikasplit-host
```

On the PC (Go only, no cgo):

```sh
go build -o bin/pikasplit-view.exe ./cmd/pikasplit-view
```

## First run (pairing)

1. On the Mac: `./bin/pikasplit-host -pair`. On first run, macOS asks for the
   **Accessibility** permission for the app running the command (Terminal,
   iTerm…). Grant it in System Settings > Privacy & Security > Accessibility,
   then restart.
2. The Mac prints the command to run on the PC, for example:
   `pikasplit-view -host 192.168.1.20:7878 -pair K7QF-2M4X-PZ3A-9HTD -pin 123456`
3. Afterwards, without pairing: `./bin/pikasplit-host` on the Mac and
   `pikasplit-view -host 192.168.1.20` on the PC.

## Usage

- Click in the window to control the Mac (that first click is not forwarded).
  Every key goes to the Mac, including Win, Alt+Tab and Alt+F4.
- **Ctrl+Alt+End**: pauses capture and gives the keyboard back to Windows. A
  click in the window resumes it.
- The Windows key maps to Cmd, Alt to Option, AltGr to right Option.
  `pikasplit-host -swap-ctrl-cmd` makes the PC's Ctrl the Cmd key (Ctrl+C =
  Cmd+C).
- `-iso=false` on the host if the Mac uses an ANSI (US) layout.

## Virtual display

While a viewer is connected, the Mac has an extra display named "pikasplit",
sized to the viewer window and resized live when the window is resized. It is
removed when the viewer disconnects, and macOS moves its windows back.

- When Windows display scaling is 150% or more, the virtual display is HiDPI
  (Retina): a 3840x2160 window gives a display that looks like 1920x1080.
  `pikasplit-view -scale 1|2` forces it.
- `pikasplit-host -max-size 5120x2880` raises the largest backing size
  (default 3840x2160 pixels).
- `pikasplit-host -virtual-display=false` drives the main display instead.
- The display uses the private `CGVirtualDisplay` API (like DeskPad and
  BetterDisplay); a future macOS release may break it.

## Development

`go test ./...` runs on both OSes. Outside macOS, `pikasplit-host` only logs
the events it receives, which lets you test the viewer on a single PC.
`PIKASPLIT_DIR` changes the configuration directory (identity, pairings).
