# pikasplit

Control a Mac from a window on a Windows PC, over the local network, so you no
longer have to switch the monitor's input.

- `pikasplit-host` runs on the Mac: it creates a virtual display sized to the
  viewer window, streams it as H.264, and injects the keyboard and mouse input
  it receives.
- `pikasplit-view` runs on the PC: it shows the Mac's display in a window and,
  while the window has focus, sends the PC's keyboard and mouse to the Mac.

Status: milestone 4 (keyboard/mouse, virtual display, video). Next: lower
latency (UDP transport, GPU decoding and rendering).

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

1. On the Mac: `./bin/pikasplit-host -pair`. macOS asks for two permissions
   for the app running the command (Terminal, iTerm…): **Accessibility** (input)
   and, when the viewer first connects, **Screen Recording** (video). Grant them
   in System Settings > Privacy & Security, then restart pikasplit-host.
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
- `pikasplit-host -virtual-display=false` drives the main display instead
  (no video).
- The display uses the private `CGVirtualDisplay` API (like DeskPad and
  BetterDisplay); a future macOS release may break it.

## Video

The Mac captures the virtual display with ScreenCaptureKit and encodes it with
the hardware H.264 encoder in low-latency mode; the PC decodes it with Media
Foundation. The window title shows the round-trip time and the frame rate. The
Mac cursor is part of the video, so the Windows cursor is hidden over it.

- `pikasplit-host -bitrate 40` sets the bitrate in Mbit/s (default 40; raise it
  if text looks soft), `-fps 60` the frame rate.
- When the network falls behind, frames are dropped until the next keyframe
  rather than piling up latency.

## Development

`go test ./...` runs on both OSes. Outside macOS, `pikasplit-host` only logs
the events it receives and, on Windows, streams an animated test pattern
encoded with Media Foundation, which lets you test the viewer on a single PC.
`PIKASPLIT_DIR` changes the configuration directory (identity, pairings).
