# Pasting Images into Claude Inside a Container

Claude Code pastes an image with **Ctrl+V** by running `wl-paste` (Wayland) or `xclip` (X11)
to read the clipboard. Inside a container neither tool is there, and the container cannot
reach your desktop's clipboard unless you let it. This page explains what a container needs
for Ctrl+V to work, how ccy does it, and how to do the same for LXC or any other container.

Where none of this is possible (a remote server, a container you cannot change), use
`imgpaste <image>` instead: it prints a block of text that recreates the image wherever it is
pasted ([play-cli-tools.yml](../playbooks.md)).

## How it works

GNOME's compositor (mutter) owns the clipboard and talks to programs through a Unix socket,
`$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY` (usually `/run/user/<uid>/wayland-0`). A program in a
container that can connect to that socket can read the clipboard like any desktop program.
`wl-paste` is that program.

GNOME does not offer the "data-control" protocol that lets a background program read the
clipboard, so `wl-paste` briefly opens a 1x1 window to get focus and then reads. Claude Code
calls it with no timeout, so if the compositor ever withheld focus, the paste would freeze.
The fix is a small wrapper that caps each call (see step 4 below).

Evidence and the options that were weighed:
[Plan 00151 survey](../../CLAUDE/Plan/Completed/00151-container-clipboard-sharing/RESEARCH-survey.md).

## What any container needs

1. **The socket, mounted read-only.** Bind-mount the socket file itself, not the whole
   `$XDG_RUNTIME_DIR`: that directory also holds the D-Bus session bus, the keyring and
   other sockets the container should not reach.

2. **Two environment variables.** `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`, pointing at where
   the socket appears inside. `WAYLAND_DISPLAY` may instead be an absolute path to the socket.

3. **A user allowed to connect.** The socket is owned by your desktop user and is
   `srwxr-xr-x`: connecting needs write permission, so only your uid (as the host sees it)
   or real root can connect.

   - Rootless Podman (ccy): the container's root is your uid on the host, so it works.
   - Privileged LXC (no `lxc.idmap`; containers made with `sudo lxc-create` are this kind):
     container uid N is host uid N. Root works; otherwise run Claude as a user with the
     same uid as your desktop user.
   - Unprivileged LXC or Docker with user namespaces: the container's uids map to a
     subordinate range that cannot connect. Map your uid through, or use `imgpaste`.

4. **`wl-clipboard` installed, with the timeout guard.** Install the distro package
   (`wl-clipboard` on Debian, Ubuntu and Fedora), then put this wrapper at
   `/usr/local/bin/wl-paste`, which comes before `/usr/bin` on `PATH`:

   ```bash
   #!/bin/bash
   timeout 5 /usr/bin/wl-paste "$@"
   rc=$?
   if [[ $rc -eq 124 ]]; then echo "wl-paste: no answer from the compositor in 5s (GNOME withheld focus?)" >&2; fi
   exit $rc
   ```

5. **SELinux.** On a Permissive host it works, and the host logs a denial for each access. On
   an Enforcing host a container domain may be refused the connection; this has not been
   tested ([ccy.md: SELinux-enforcing hosts](../ccy.md#selinux-enforcing-hosts)).

## ccy

Built in from CCY 3.75.0 (container 2.41). The launcher mounts the socket read-only and sets
both variables, and the image carries `wl-clipboard` and the wrapper. Nothing to configure.

## Podman or Docker (`run`)

```bash
podman run --rm -it \
  -v "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:ro" \
  -e WAYLAND_DISPLAY -e XDG_RUNTIME_DIR \
  <image> bash
```

Run it as your desktop user (rootless Podman). Rootful Docker runs as real root, which can
connect; with user-namespace remapping enabled it cannot.

## LXC

Not tested, and not automated: this repo does not manage LXC container configs
([Plan 00151](../../CLAUDE/Plan/Completed/00151-container-clipboard-sharing/PLAN.md), Task 2.2).

1. Add to `/var/lib/lxc/<name>/config`, replacing `1000` with your desktop user's uid:

   ```
   lxc.mount.entry = /run/user/1000/wayland-0 run/host-wayland/wayland-0 none bind,ro,create=file,optional 0 0
   ```

   `optional` lets the container start when you are not logged in (the socket does not exist
   then).

2. Inside the container, for the user who runs Claude:

   ```bash
   export WAYLAND_DISPLAY=/run/host-wayland/wayland-0
   ```

3. Install `wl-clipboard` and the wrapper inside the container (step 4 above).

4. **After every logout and login, restart the container.** GNOME makes a new socket on each
   login, and the bind mount still points at the old one, so `wl-paste` fails with
   "connection refused" until the container restarts.

A long-lived system container holding your compositor socket is a bigger exposure than a
ccy session, which ends with its terminal: anything running in it can open windows on your
desktop and read the clipboard.

## Checking it works

Copy an image, then inside the container:

```bash
wl-paste -l                          # lists image/png
wl-paste --type image/png | wc -c    # a byte count above 0
```

Exit code 124 means the compositor did not answer in 5 s. "Failed to connect to a Wayland
server" means the socket or the variables are missing, or the user may not connect (step 3).
