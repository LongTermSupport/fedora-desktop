# Plan 00151 survey: Claude Code image paste inside ccy and LXC

Every claim is tagged **VERIFIED** (evidence read or run in this session: file:line,
command output, or upstream source read) or **INFERRED** (reasoned from evidence, not
observed). Items that could not be established are listed in the last section.

Environment the facts were gathered in: a ccy container (rootless Podman, Debian 12
bookworm base), Claude Code 2.1.288, on a Fedora GNOME Wayland host.

---

## 1. How Claude Code reads the clipboard on Linux

Claude Code ships as a single Bun executable
(`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`, version 2.1.288).
The JS bundle is embedded in it as plain text, so the snippets below were pulled out of the
binary with a stdlib Python byte search. **VERIFIED.**

### 1.1 Key binding

`chat:imagePaste` is bound to `ctrl+v` on Linux (`alt+v` on Windows, and `ctrl+v` as well on
WSL). **VERIFIED**, from the binary:

```js
Se=ve?"alt+v":"ctrl+v"
... [Se]:"chat:imagePaste",...N==="wsl"&&{"ctrl+v":"chat:imagePaste"} ...
```

### 1.2 Image read: shell commands with fixed fallbacks and no env-var gate

The image path is a pair of `sh` command strings run through execa (`mR(e,{reject:!1})`).
**VERIFIED**, from the binary:

```js
linux:{
  checkImage:`xclip -selection clipboard -t TARGETS -o 2>/dev/null | grep -E "image/(png|jpeg|jpg|gif|webp|bmp)" || wl-paste -l 2>/dev/null | grep -E "image/(png|jpeg|jpg|gif|webp|bmp)"${b}`,
  saveImage:`xclip -selection clipboard -t image/png -o > ${l} 2>/dev/null || wl-paste --type image/png > ${l} 2>/dev/null || xclip -selection clipboard -t image/bmp -o > ${l} 2>/dev/null || wl-paste --type image/bmp > ${l}${x}`,
  getPath:"xclip -selection clipboard -t text/plain -o 2>/dev/null || wl-paste 2>/dev/null",
  deleteFile:`rm -f -- ${l}`}
```

and the caller:

```js
if((await u(i.checkImage)).exitCode!==0)return null;
...mkdir(E(s),{mode:448})...
if((await u(i.saveImage)).exitCode!==0) return m("clipboard_read","save_failed"),null;
let p=await ie().readFileBytes(s);
if(p.length>=2&&p[0]===66&&p[1]===77) p=await(await sW())(p).png().toBuffer();   // BMP -> PNG
```

What this means:

- **Commands used:** `xclip -selection clipboard -t TARGETS -o` and `wl-paste -l` to detect an
  image; then `xclip ... -t image/png -o`, `wl-paste --type image/png`, then the same for
  `image/bmp`. xclip is always tried first and wl-paste second. **VERIFIED.**
- **MIME types:** detection accepts `image/png|jpeg|jpg|gif|webp|bmp` from the type list, but
  the save step only asks for `image/png` and then `image/bmp`. A clipboard that offers only
  `image/jpeg` passes detection and then fails with `save_failed`. **VERIFIED** (from the
  strings). **INFERRED** that this is what the user sees for a JPEG-only clipboard.
- **Env vars:** the image path checks **no** environment variable. It does not look at
  `WAYLAND_DISPLAY` or `DISPLAY`; it runs the command chain and keys on exit codes. So a
  `wl-paste` found first on `PATH` is called whatever the display situation is. **VERIFIED.**
- **When the tools are absent:** both commands fail with "not found", so checkImage exits
  non-zero, and the user gets `No image found in clipboard. Use <key> to paste images.` (or
  `You're SSH'd; try scp?` when Claude detects SSH). **VERIFIED** that the strings exist.
  **INFERRED** which one is shown when.
- **No timeout on the image commands:** `u()` passes only `{reject:!1}`. A `wl-paste` that
  hangs waiting for focus (section 4) would hang the paste. **INFERRED.**
- **Temp file:** the image is staged as `<tmp>/claude-<uid>/claude_cli_latest_screenshot.png`
  (`hl()` → `p(Yb(),"claude-"+uid)`), then deleted. **VERIFIED.**

### 1.3 Text clipboard and the native addon (not used for images)

- Text read (`_mt`) tries `wl-paste --no-newline`, then `xclip -selection clipboard -o`, then
  `xsel --clipboard --output`, then a native addon, `getLinuxClipboardText`. **VERIFIED.**
- The addon (`clipboard-napi.node`) embeds `wl-clipboard-rs-0.9.3` and `x11rb`, with
  `zwlr_data_control_offer_v1` and `ext_data_control_device_v1` symbols. It exports only
  `getLinuxClipboardText` and `setLinuxClipboardText`; nothing for images. **VERIFIED.**
  wl-clipboard-rs works only over a data-control protocol, which GNOME lacks (section 3), so
  the addon cannot help on GNOME either. **INFERRED.**
- Copy (the other direction) probes `wl-copy` only when `WAYLAND_DISPLAY` is set, and xclip or
  xsel only when `DISPLAY` is set. Screenshot-to-clipboard uses `xclip -t image/png -i` only.
  **VERIFIED.** Not in scope here.

### 1.4 Pasting an image *path* already works with no clipboard tool

When bracketed-paste text contains a token that ends in `.png|.jpe?g|.gif|.webp` and is an
absolute path that exists, Claude reads the file and attaches it as an image (`Xko`/`Jko`,
regex `Bat=/\.(png|jpe?g|gif|webp)$/i`, `if(W(s))r=await ie().readFileBytes(s)`).
**VERIFIED** from the code. This is the route a file drag-and-drop into the terminal takes.
Inside a container it works only if the pasted path exists **inside** the container, and ccy
mounts the project at `/workspace`, not at its host path. **VERIFIED**
(`claude-yolo:2121`, `-v "$PWD:/workspace..."`).

---

## 2. What ccy already passes in

### 2.1 Launcher

`files/var/local/claude-yolo/claude-yolo:3083-3109` ("Detect Wayland or X11"). **VERIFIED:**

- With `WAYLAND_DISPLAY` set, it bind-mounts **only** `$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY`,
  read-only, at the same path, and sets `WAYLAND_DISPLAY`, `XDG_RUNTIME_DIR` and `DISPLAY=:0`.
  The comment (SEC-03/CCY-02) explicitly refuses to mount the whole runtime dir (D-Bus session
  bus, PipeWire, keyring).
- `DISPLAY=:0` is set but no X11 socket is mounted on the Wayland branch. The `/tmp/.X11-unix`
  mount is made only when there is no Wayland. The default network is `--network podman`
  (`claude-yolo:2884`), a separate network namespace, so the host's abstract
  `@/tmp/.X11-unix/X0` is not reachable either. `/proc/net/unix` inside showed no X11 or
  wayland entries. So `xclip` in the container would fail and fall through to `wl-paste`.
  **VERIFIED.**
- No `--userns` flag. Rootless default: the host user maps to container root. **VERIFIED**
  (`/proc/self/uid_map` reads `0 <host-uid> 1`, then a subuid range).
- SELinux: `ccy_selinux_mode` (`lib/common.bash:69-102`) relabels the workspace (`:z`) on
  Enforcing or Permissive hosts. The display socket is **not** relabelled.
  `docs/ccy.md:431-435` says an Enforcing host makes the display sockets unreachable inside,
  and Permissive logs an AVC per access. `--ssh-agent` mode runs with `label=disable`
  (`claude-yolo:1061-1065,1107`). **VERIFIED** (the code and doc text).

### 2.2 Inside this container (measured)

| Item                         | Value seen                                                                   | Tag      |
| ---------------------------- | ---------------------------------------------------------------------------- | -------- |
| `WAYLAND_DISPLAY`            | `wayland-0`                                                                  | VERIFIED |
| `XDG_RUNTIME_DIR`            | `/run/user/<host-uid>`                                                       | VERIFIED |
| `DISPLAY`                    | `:0`                                                                         | VERIFIED |
| Socket                       | present, `srwxr-xr-x root root` (host user shown as root through the userns) | VERIFIED |
| Socket SELinux label         | `unconfined_u:object_r:user_tmp_t:s0`                                        | VERIFIED |
| This process's SELinux label | `system_u:system_r:container_t:s0:c…,c…` (labelling is on, not disabled)     | VERIFIED |
| `wl-paste`, `xclip`, `xsel`  | none installed                                                               | VERIFIED |
| Host enforcing mode          | not readable from inside (`/sys/fs/selinux/enforce` absent, no `getenforce`) | UNKNOWN  |

### 2.3 Images

- `files/var/local/claude-yolo/Dockerfile`: no `wl-clipboard`, `xclip` or `xsel`. The base is
  `node:lts-slim` (Debian; bookworm here). The Chromium runtime libs are installed at lines
  137-156. **VERIFIED.**
- `.claude/ccy/Dockerfile` (this repo's project image): none either. **VERIFIED.**
- Debian packages: bookworm `wl-clipboard 2.1.0-0.1+b1`, trixie `2.2.1-2`
  ([packages.debian.org](https://packages.debian.org/search?keywords=wl-clipboard&searchon=names&exact=1)).
  **VERIFIED.**
- Any Dockerfile change needs `REQUIRED_CONTAINER_VERSION` (`claude-yolo:73`, currently
  `2.40`) and the Dockerfile `claude-yolo-version` label bumped together, plus a
  `CCY_VERSION` bump (`claude-yolo:17`). **VERIFIED** (the comments at `claude-yolo:8,68-72`).

---

## 3. The Wayland connection works from inside ccy (probe)

Script: `CLAUDE/Plan/00151-container-clipboard-sharing/probe-wayland-globals.py`. It uses only
the stdlib: it connects, sends `wl_display.get_registry` and `wl_display.sync`, prints every
`wl_registry.global`, and disconnects. It binds nothing and creates no surface.

Run inside this ccy container. **VERIFIED**, output abridged to the relevant lines:

```
socket: /run/user/<host-uid>/wayland-0
peer credentials as seen here: pid=0 uid=0 gid=0
5   wl_data_device_manager                    3
7   zwp_primary_selection_device_manager_v1   1
9   xdg_wm_base                               7
10  gtk_shell1                                7
16  wl_seat                                   10
28  xdg_activation_v1                         1
... (41 globals in total)
--- clipboard-relevant ---
wl_data_device_manager: PRESENT
zwlr_data_control_manager_v1: absent
ext_data_control_manager_v1: absent
zwp_primary_selection_device_manager_v1: PRESENT
gtk_primary_selection_device_manager: absent
```

Findings:

1. **The connection succeeds** from a `container_t` process, through a `:ro` bind mount,
   across the user namespace. A read-only bind does not block `connect()`. **VERIFIED.** The
   peer shows `pid=0` (the compositor is outside the pid namespace) and `uid=0` (the host user,
   mapped). Wayland does no uid check of its own. **VERIFIED** (the connection was accepted
   and served).
2. **This mutter offers no data-control protocol.** Neither `zwlr_data_control_manager_v1` nor
   `ext_data_control_manager_v1` is advertised. So `wl-paste` must use the focus trick
   (section 4). **VERIFIED.** This matches upstream reports that mutter implements neither
   protocol ([search summary](https://github.com/CherryHQ/cherry-studio/pull/21244)). **INFERRED**
   that this holds for the host's mutter version generally.
3. `gtk_shell1` and `xdg_activation_v1` are present. wl-clipboard uses both to ask for focus.
   **VERIFIED.**
4. Side finding: **no `zwp_virtual_keyboard_manager_v1`** is advertised. `wtype`, which this
   repo's `clean-paste` uses (`files/home/.local/bin/clean-paste:65`), needs that protocol.
   So keystroke injection is not available on this GNOME. **VERIFIED** (the global is
   absent). **INFERRED** that `clean-paste`'s Ctrl+Shift+V injection fails here. Outside
   this plan's scope; recorded for the owner.
5. The probe does not prove that SELinux allows it on an *Enforcing* host. This session's host
   mode is unknown from inside. If the host is Enforcing, the docs claim at
   `docs/ccy.md:431-433` is contradicted by this result. If it is Permissive, every connect
   logs an AVC. **UNKNOWN.** The owner can settle it on the host (section 7, step 1).

---

## 4. wl-clipboard under GNOME (no data-control)

Upstream source read at `bugaevc/wl-clipboard` master (commit `16cf9d3f`). **VERIFIED:**

- `src/types/registry.c:156-171`: it prefers `ext_data_control_manager_v1`, then
  `zwlr_data_control_manager_v1`, then falls back to `wl_data_device_manager`, which "requires
  us to use the popup surface hack".
- `src/wl-paste.c:537-550`: with `needs_popup_surface`, it creates a popup surface. "When it
  gets focus, we'll immediately get the selection events." `--watch` is refused in this mode
  (`complain_about_watch_mode_support`), so **no clipboard watching on GNOME**.
- `src/types/popup-surface.c:62-163`: a 1×1 fully transparent ARGB toplevel
  (title `wl-clipboard`, app id `io.github.bugaevc.wl-clipboard`). It asks for focus with
  `gtk_surface1_present(surface, 0)` when `gtk_shell1` exists, and with
  `xdg_activation_v1_activate` **only if** `XDG_ACTIVATION_TOKEN` or `DESKTOP_STARTUP_ID` is
  set in the environment.
- Man page, BUGS (`data/wl-clipboard.1:178-184`): "it will briefly pop up a tiny transparent
  surface (window) ... this can cause visual issues such as brief flashing. In some cases the
  Wayland compositor doesn't give focus to the popup surface, which prevents wl-clipboard from
  accessing the clipboard and manifests as a hang."

What users see on GNOME. **INFERRED** from upstream reports, not tested here:

- A brief focus change: the terminal loses keyboard focus for an instant, then gets it back
  when the surface is destroyed. Claude's image paste runs wl-paste **twice** (`-l`, then
  `--type image/png`), so two flashes per Ctrl+V. Reports:
  [wl-clipboard #31](https://github.com/bugaevc/wl-clipboard/issues/31) (a window flashes),
  [ddterm #672](https://github.com/ddterm/gnome-shell-extension-ddterm/issues/672) (it steals
  focus from a drop-down terminal), [wl-clipboard #12](https://github.com/bugaevc/wl-clipboard/issues/12)
  (it hangs when the compositor will not focus the new surface).
- A risk of hanging, if mutter's focus-stealing prevention declines a `present` with
  timestamp 0. Combined with Claude's lack of a timeout (1.2), that would freeze the paste.
  Whether mutter on this host grants focus: **UNKNOWN** until tried.
- wl-paste on GNOME is in common use, and this repo already relies on host-side `wl-paste`
  under GNOME (`play-clean-paste.yml:48-54`, `clean-paste:41`). **INFERRED** that the trick
  works on this desktop for a host client.

From a different uid namespace: the Wayland protocol has no uid concept. The surface buffer
(an anonymous shm fd) and the `wl_data_offer.receive` pipe fd are passed with `SCM_RIGHTS`,
which works across user namespaces. ccy's headed Chromium already maps windows over this
same socket (Plan overview; `Dockerfile:162-167`). **INFERRED** that a container wl-paste
behaves exactly like a host wl-paste. Nothing namespace-specific was found.

---

## 5. LXC containers here

- `playbooks/imports/play-lxc-install-config.yml` installs LXC, lxc-net, the firewall and
  bridge, an SSH key and config, `container-selinux`, and clones `lxc-bash`. lxc-bash holds
  completion and a copy script only; it creates no containers. **No container config is
  managed by IaC**: no `/etc/lxc/default.conf` template, no `lxc.idmap`, no `lxc.mount.entry`,
  no subuid/subgid. **VERIFIED** (grep of the play; the lxc-bash tree listing).
- Containers are created as root (`sudo lxc-create -t download …`, in `docs/containerization.md:92`
  and `files/var/local/docker-in-lxc:392`), with no idmap, so they are **privileged**.
  Container uid N is host uid N. **INFERRED** (no idmap anywhere in the repo; the docs say
  "Privileged by default", `docs/containerization.md:58`). `docker-in-lxc`'s help text calls
  its containers "unprivileged" (`docker-in-lxc:59,100,108`), but its create path adds no
  idmap. That text looks wrong. **INFERRED.**
- The only existing mount entry pattern is `docker-in-lxc:400-401`, which appends
  `lxc.mount.entry = <project_dir> mnt/project none bind,create=dir 0 0` to
  `/var/lib/lxc/<name>/config`. **VERIFIED.**
- `lxc-start` transitions to `container_runtime_t`, and the payload to a container domain
  (play comment, lines 63-69). **VERIFIED** (the comment). The effect on a `connectto` to the
  compositor is **UNKNOWN**, the same open question as ccy.

What bind-mounting the Wayland socket into LXC would take (**INFERRED**, not tried):

1. `lxc.mount.entry = /run/user/<uid>/wayland-0 run/host-wayland/wayland-0 none bind,ro,create=file,optional 0 0`,
   plus `WAYLAND_DISPLAY=/run/host-wayland/wayland-0` (an absolute path is allowed) for the
   user who runs Claude.
2. **Permissions:** the socket is `srwxr-xr-x`, owned by the host user. `connect()` needs
   write permission on the socket, so in a privileged container only **root or the same
   numeric uid** can connect. The in-container user who runs Claude must have the host user's
   uid, or the socket must be re-permissioned (host-side, and rejected).
3. **Lifetime:** LXC containers are long-lived and may autostart before login. A bind mount
   pins the socket inode. When the session ends and mutter recreates the socket on the next
   login, the container keeps a dead inode (`ECONNREFUSED`) until it restarts. Without
   `optional`, a container started before login fails to start. This is a real operational
   cost, and ccy (per-session, `--rm`) does not have it.
4. Exposure: the same as ccy (section 6, A), but for a long-lived, broadly provisioned system
   container.
5. Nothing installs tools **inside** generic LXC containers through this repo, so wl-clipboard
   (or a shim) inside them is outside current IaC. `docker-in-lxc` provisions via
   `lxc-attach` and could carry it for its containers only.

---

## 6. Options

### A. wl-clipboard in the ccy image, over the existing socket

- **Works on GNOME:** probably, with a flash and focus blip per paste (two per Ctrl+V) and a
  hang risk. **INFERRED**; the connection itself is **VERIFIED** (section 3).
- **Exposure:** **adds nothing new.** The container already holds the compositor socket,
  which advertises `wl_data_device_manager` to it (VERIFIED). Any process in the container
  can already open a surface, take focus and read the clipboard: wl-paste is a convenience,
  not a new capability. The existing risk ("the display socket is not confined",
  `docs/ccy.md:413-414`) stays as it is. **INFERRED** (protocol reasoning).
- **Repo changes:** add `wl-clipboard` to the apt list in
  `files/var/local/claude-yolo/Dockerfile` (lines 137-156); bump the Dockerfile label,
  `REQUIRED_CONTAINER_VERSION` (`claude-yolo:73`) and `CCY_VERSION` (`claude-yolo:17`);
  add a line to `docs/ccy.md`. No launcher logic changes.
- **Serves LXC:** only with option B.
- **Caveat:** the JPEG-only clipboard gap from 1.2 is Claude's, not wl-clipboard's.

### B. The same for LXC, via a socket mount entry

- **Works on GNOME:** as A, if the in-container uid matches and the container started after
  the current login. **INFERRED.**
- **Exposure:** the compositor socket (window creation, clipboard read on focus, input to
  its own surfaces) inside a long-lived, privileged system container. Larger than ccy, because
  the container persists and is not scoped to one session.
- **Repo changes:** none of the LXC config is IaC today. It needs either a new managed config
  snippet (e.g. an `lxc.include` the play drops, with per-container opt-in), or
  `docker-in-lxc` changes for its own containers, plus installing wl-clipboard inside each
  container. The socket-lifetime problem (5.3) has no clean fix without mounting the runtime
  dir, which ccy's SEC-03 rule rejects.
- **Verdict:** feasible but fragile. Not recommended as the first step.

### C. Host-side bridge that writes the image to a file the container can read

- **Host reader on GNOME:**
  - Host `wl-paste --type image/png` (wl-clipboard is already on the host via
    `play-clean-paste.yml`). It has the same focus trick as A, so no better than A on GNOME.
    **INFERRED.**
  - Host `xclip -selection clipboard -t image/png -o` against Xwayland (`DISPLAY=:0`). X11
    selection reads need no focus, and mutter bridges the Wayland clipboard to Xwayland
    clients. **INFERRED**; xclip is not installed by IaC on the host (grep found none).
  - A GNOME Shell extension (`St.Clipboard.get_content` or `Meta.Selection`
    `owner-changed`). This is the only reader that can *watch* without focus. But
    `st_clipboard_get_content()` with `image/png` has an open bug that returns null
    ([gnome-shell #4034](https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/4034), reported
    on 3.38/X11). **UNKNOWN** on the current version.
- **Trigger:** `wl-paste --watch` is impossible on GNOME (section 4), so the bridge is either
  a host keybinding ("send clipboard image to containers"), the extension above, or a
  `systemd --user` path unit on GNOME's screenshot folder (GNOME's screenshot UI saves a file
  and copies to the clipboard; **INFERRED**, not checked here). Automatic keystroke
  injection afterwards is not available (no virtual-keyboard protocol, section 3).
- **Exposure:** one host directory holding only the images the user chose to send. No socket.
  The smallest exposure of all options.
- **Delivery into Claude without D:** the user pastes a **path** (1.4). That needs the path to
  exist in the container: for ccy, a spool under the project dir (`untracked/…`), which the
  host side cannot locate without knowing the focused project; or a fixed spool mounted at
  the same absolute path in every container. Usable, but it is not Ctrl+V.
- **Serves both:** yes. The same host directory can be bind-mounted read-only into ccy (a new
  `-v` in `GUI_MOUNTS`) and into LXC (a `lxc.mount.entry`; uid is no problem for a privileged
  container reading a 0644 file).

### D. Shim `wl-paste` in the container, fed from a host spool (C + D)

- **Mechanism:** a small script installed as `/usr/local/bin/wl-paste` in the container (not
  the real wl-clipboard). Claude calls only `wl-paste -l` and `wl-paste --type image/png|image/bmp`
  for images, and `wl-paste [--primary] --no-newline` for text (1.2, 1.3). The shim answers
  `-l` with `image/png` when the spool image is fresh, `--type image/png` with the file, and
  exits non-zero for everything else, so text reads fall through to xclip, xsel and the addon
  as now. Claude then pastes natively with Ctrl+V. **VERIFIED** that Claude has no env gate,
  so a PATH shim is called (1.2).
- **Staleness:** without a watcher (impossible on GNOME except via an extension), the shim
  cannot tell that the clipboard has since moved on to text. Options: serve only if the file
  is newer than N seconds; or the host bridge writes on an explicit keybinding. That makes
  the flow "copy, press the host shortcut, Ctrl+V". **INFERRED** design constraint.
- **On-demand variant** (the shim asks a host helper over a mounted unix socket, and the
  helper runs a host reader): it gives fresh data with no staleness. But the host reader is
  either the same focus trick (no gain over A for ccy) or xclip via Xwayland (focus-free,
  **INFERRED**). It also adds a host user service and a socket into the container, which
  needs the same SELinux `connectto` that is unproven for the compositor.
- **Exposure:** as C (spool) or C plus one narrow socket (on-demand). The compositor socket
  is not needed at all, so it suits LXC.
- **Repo changes:** the shim (a file under `files/var/local/claude-yolo/`, installed in the
  Dockerfile); a spool `-v` in `claude-yolo` near `GUI_MOUNTS` (`3085-3109`); the host bridge
  (a new `files/home/.local/bin/` script, plus a GNOME keybinding play modelled on
  `play-clean-paste.yml:99-137`); for LXC, a mount entry and the shim inside each container
  (not IaC today). Version bumps as in A.
- **Serves both:** yes, which is the main argument for it.

### E. Other routes found

- **E1. Path paste / drag-and-drop (no changes):** dragging an image file into the terminal
  pastes its path, and Claude attaches it if the path exists inside the container (1.4).
  ccy: a dragged file pastes its **host** path, but the container sees the project at
  `/workspace/...`, so the path does not resolve and nothing is attached. **INFERRED** that
  it fails for ccy unless the host path is also mounted at the same path. A path typed by
  hand as `/workspace/<file>` works today. LXC: works only with a same-path mount.
- **E2. X11 via Xwayland:** mount `/tmp/.X11-unix/X0` and the mutter Xauthority cookie, and
  install `xclip`. Focus-free reads. But it grants full X11 access to every Xwayland client
  (keystroke snooping and injection into X apps), and Xwayland may start on demand.
  **Exposure is strictly worse than A.** Not recommended. **INFERRED.**
- **E3. OSC 52 read:** Claude does not read images via OSC 52, and terminals generally do not
  allow OSC 52 reads. Not viable. **INFERRED.**
- **E4. `imgpaste` (Plan 00150):** stays the route for remote and SSH work, and the fallback
  for everything.

### Summary table

| Option                    | GNOME works?                         | New exposure                         | ccy | LXC | Repo cost |
| ------------------------- | ------------------------------------ | ------------------------------------ | --- | --- | --------- |
| A wl-clipboard in ccy     | probably; flash/blip, hang risk      | none beyond the mounted socket       | yes | no  | small     |
| B socket into LXC         | as A; breaks across logins           | compositor socket in a long-lived CT | no  | yes | medium    |
| C host spool + path paste | host side yes; not Ctrl+V            | one image dir                        | yes | yes | medium    |
| D shim + spool            | yes (no compositor use in container) | one image dir                        | yes | yes | medium    |
| E2 Xwayland               | likely                               | full X11 access (worse)              | yes | yes | medium    |

---

## 7. Recommendation and smallest prototype

**Recommendation:**

1. **ccy: option A.** It adds no exposure the container does not already have, needs one
   package and the standard version bumps, and Claude's own Ctrl+V code path is used as is.
   The only open question is whether mutter focuses wl-paste's popup reliably when it is
   launched from a container (UX: blip versus hang). Prove that first.
2. **LXC: not B.** If LXC image paste is wanted, use **D** (shim plus a read-only host spool,
   filled by a host keybinding). It keeps the compositor socket out of long-lived containers,
   has no socket-lifetime problem, and the same shim and spool could later replace A in ccy
   if A's UX proves poor. Owner decision: is LXC worth a host shortcut press per image, given
   that `imgpaste` already covers it?

**Smallest prototype for A.** These steps are for the owner, run once on the host. They change
no host state beyond building one throwaway local image. It should be written as a plan-local
script (`prototype-ccy-wl-paste.bash`, R1-R14 compliant) in the prototype phase.

1. SELinux fact, read-only:
   `getenforce` and `sudo ausearch -m avc -ts recent --no-pager 2>/dev/null | grep -i wayland | cat`.
   If Enforcing, and the in-container probe above connected, then the `docs/ccy.md:431-433`
   claim is wrong and must be corrected in the deliver phase.
2. Build a throwaway image from the current ccy image, adding wl-clipboard: a two-line
   Containerfile in the plan folder (`FROM <ccy image>` / `RUN apt-get update && apt-get install -y --no-install-recommends wl-clipboard`), run with `podman build -t ccy-wlpaste-proto`.
3. Copy a screenshot to the clipboard (Print, then copy).
4. Run, with the same socket flags the launcher uses:
   `podman run --rm -it -v "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:ro" -e WAYLAND_DISPLAY -e XDG_RUNTIME_DIR ccy-wlpaste-proto bash -c 'timeout 10 wl-paste -l; echo "list rc=$?"; timeout 10 wl-paste --type image/png | wc -c; echo "png rc=${PIPESTATUS[0]}"'`.
   Record: the types listed, the byte count, whether a window flashed, and whether focus
   returned to the terminal. An `rc=124` means mutter withheld focus (the hang case).
5. If step 4 succeeds, start `claude` in that same container and press Ctrl+V with an image on
   the clipboard. That proves the end-to-end path.
6. `podman rmi ccy-wlpaste-proto`.

If step 4 hangs (rc 124) or the flashing is unacceptable, fall back to D for both container
types, and prototype the host reader first (`xclip` via Xwayland, or host `wl-paste`, from a
keybinding).

---

## Could NOT be established

- **The host's SELinux mode.** Whether Enforcing would deny the compositor `connectto` that
  succeeded here: is the host Permissive, or does container-selinux allow it?
- **Whether mutter grants focus** to wl-paste's popup from a container (or from the host),
  and what the blip looks like. That needs a real run (prototype step 4). The probe did not
  create a surface or read the clipboard, so as not to touch the owner's desktop.
- **The host's GNOME/mutter version** (the advertised globals are recorded above, the version
  is not).
- **Whether `St.Clipboard.get_content(..., 'image/png')` works** on the current gnome-shell
  (issue #4034 is old and X11-only).
- **Whether GNOME's screenshot UI on this host** saves to a folder as well as the clipboard.
- **The actual LXC containers on the host** (their config files, privileged or not, the
  in-container user's uid). Only the repo's creation paths were read. The container configs
  under `/var/lib/lxc/` are not visible from here.
- **Whether bookworm's `wl-clipboard 2.1.0`** has the `gtk_shell1`/`xdg_activation` focus
  requests that master has. Upstream master was read, not the 2.1.0 tag. The trixie package
  is 2.2.1.
