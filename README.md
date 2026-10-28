# pikasplit

Control a Mac from a window on a Windows PC, over the local network, so you no
longer have to switch the monitor's input.

- `pikasplit-host` runs on the Mac: it receives keyboard and mouse input and
  injects it.
- `pikasplit-view` runs on the PC: while its window has focus, the PC's
  keyboard and mouse go to the Mac.

Status: milestone 2 (keyboard/mouse only). The Mac's video in the window comes
in later milestones.

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

## Development

`go test ./...` runs on both OSes. Outside macOS, `pikasplit-host` only logs
the events it receives, which lets you test the viewer on a single PC.
`PIKASPLIT_DIR` changes the configuration directory (identity, pairings).
