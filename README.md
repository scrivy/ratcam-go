Ratcam
=====

simple webcam server that streams jpegs over http.

requires linux, go, and a V4L2 compatible webcam.

tested with a Logitech C920 HD Pro on a rapsberry pi, cubieboard 2, espressobin, and odroid hc2 with arch linux.

## Systemd

- copy ratcam.service to /lib/systemd/system/ratcam.service
- `sudo systemctl daemon-reload`
- `sudo systemctl start ratcam`
- `sudo systemctl status ratcam`
- `sudo systemctl stop ratcam`
- `sudo systemctl enable ratcam`
- restart system

