# Review: plan-00199/install-play (tip 6128249f) — md-viewer install play

Reviewer: Fable 5.1, adversarial pre-merge, read-only. Diff: `F44...origin/plan-00199/install-play`.

## Verdict: MERGE

Nothing found that would make the host run fail or leave the other Go builds different. Two
Lows worth a follow-up commit, none blocking.

## Evidence gathered (all from the container, today)

- **Repository is public.** `gh repo view LongTermSupport/md-viewer` → `visibility: PUBLIC`,
  default branch `main`. Pinned commit `b2d1f942fcdd…` exists on it ("Mark M10 done; say that
  M10 reworded SPEED-6", 2026-10-15T15:57:40Z). The repo has no tags (`/tags` is empty), so
  the full-hash pin and the "no tags yet" note in version-pins are correct.
- **No private dependency at build time.** md-viewer `go.mod`: module
  `github.com/LongTermSupport/md-viewer`, `go 1.26.9`, requires `gioui.org v0.10.3`,
  `goldmark v1.8.6`, `golang.org/x/image v0.26.0` plus indirects. No `tool` directive, no
  go-qa-ci reference; go-qa-ci appears only as `qa/go-qa-ci.ref` and `qa/go-qa.toml`, which
  `go install` never reads. No GOPRIVATE/GONOSUMDB/git auth needed.
- **Proxy and sumdb serve it.** `proxy.golang.org/…/@v/b2d1f942fcdd.info` returns
  `v0.0.0-20261015155740-b2d1f942fcdd` with the full hash in `Origin`;
  `sum.golang.org/lookup/…@v0.0.0-20261015155740-b2d1f942fcdd` → HTTP 200.
- **The exact command the task renders works through resolution.** Ran
  `GOTOOLCHAIN=local GOFLAGS=-mod=readonly GOBIN=<scratch> go install -trimpath github.com/LongTermSupport/md-viewer/cmd/md-viewer@b2d1f942fcdd8508f93a5ca40807a387032b02ae`
  from a directory holding an unrelated go.mod (the scratch dir, as the task does). It
  downloaded every module and failed only at cgo: `pkg-config: executable file not found`
  (twice: `gioui.org/app/internal/xkb`, `gioui.org/internal/vk`). So `-mod=readonly` in GOFLAGS
  is accepted with a version suffix, the cwd's go.mod plays no part, and the only missing
  pieces are the -devel packages the play installs first.
- **`--version` carries the hash prefix.** `internal/app/version.go` prints
  `md-viewer <info.Main.Version>`; for a `go install pkg@hash` build that is the pseudo-version
  `v0.0.0-20261015155740-b2d1f942fcdd`, which contains `md_viewer_version[:12]`. The assert
  holds.
- **Gio v0.10.3 cgo requirements vs the dnf list.** From the module cache, the
  `#cgo linux pkg-config:` lines name: `egl wayland-egl`, `wayland-client`,
  `wayland-client wayland-cursor`, `x11 xkbcommon xkbcommon-x11 x11-xcb xcursor xfixes`,
  `xkbcommon`; headers included: EGL, X11 (+Xcursor, Xfixes, Xlib-xcb), wayland, xkbcommon,
  `vulkan/vulkan.h`. No GLES header is included (GL is loaded through EGL at run time). The
  play's eleven packages cover exactly that set; every package name resolves on Fedora 44
  (mdapi f44): gcc 16.0.1, pkgconf-pkg-config 2.5.1, wayland-devel 1.26.0,
  libxkbcommon-devel / libxkbcommon-x11-devel 1.13.1, libX11-devel 1.8.13,
  libXcursor-devel 1.2.3, libXfixes-devel 6.0.1, libglvnd-devel 1.7.0,
  vulkan-headers <ip>, desktop-file-utils 0.28. Vulkan is dlopen'ed (`-ldl`), so no loader
  package is needed.
- **Go toolchain.** Fedora 44 `golang` is 1.26.9-1.fc44 in both f44 and f44-updates; bodhi shows
  it stable since 2026-10-10 (1.26.8 before that, 2026-09-08). go.dev lists go1.26.9 as the
  newest 1.26 patch. So `GOTOOLCHAIN=local` is satisfiable on an up-to-date F44 host.
- **Shared task file leaves existing builds identical.** Rendered the F44 and branch
  `argv`/`chdir` expressions through ansible-playbook for ccy-sessions, ccy-teams (with
  ldflags), ftp-camera, vmtest, podfreeze, lxcfreeze: `same=True`, argv is a list, for all six.
  md-viewer renders `['go','install','-trimpath','<module>@<hash>']` with chdir
  `~/.cache/ccy-go-build` (created by the preceding task in the same file).
- **New assert expressions** evaluate `[true, true, true]` for the real module string
  (`basename` on the split string works).
- **Syntax.** `ansible-playbook --syntax-check` of the branch play passes (from the project's
  config). `scripts/qa-version-pins.bash` on the branch tree: `VERSION-PINS-OK 12 pin(s)`,
  coverage 12 of 12.
- **Idempotency.** dnf `present`; `go install` re-runs each time with `changed_when: false`
  and the checksum-compared `copy` reports the only real change (same design as every other
  caller); `--version` is `changed_when: false`; `copy` of the desktop file with
  `validate: desktop-file-validate %s`; `update-desktop-database` gated on that copy's
  `changed`; `community.general.ini_file` (installed, 11.4.2) is declarative on one key, so a
  second run is all-ok. All user-side tasks run `become: false` under the play's
  `become: true`, as the other callers do.
- **Fail-fast.** No `failed_when: false`, no `ignore_errors`, no debug-and-continue; an
  older golang fails inside `go` with its own message.
- **IaC placement.** Optional `scope: gnome` play under `optional/common` (ten siblings use
  that scope), Go toolchain via the existing `play-golang.yml` import, build via the shared
  task file rather than a second build path, docs row in alphabetical position, version-pins
  row with a `why:` for `untracked`, meta-deploy entry with a comment. No install-specific
  names anywhere in the diff.
- **meta-deploy entry** describes what the play does accurately (dnf libs, toolchain, build at
  the pin, `--version` check, desktop file, text/markdown default).
- **PLAN.md / journal / report fact-check.** "M0 to M10 merged on main; M10 opens a Gio
  window": true at the pin (`3e77dcbd Merge M10: the window and the binary`, then
  `b2d1f942`). "Own hooks daemon": `.claude/hooks-daemon.yaml`, `hooks/`, `rules/` present in
  the repo. "Fedora 44 ships golang 1.26.9": true. "README lists mesa-libGLES-devel for
  Fedora": true (README line 42). "MANUAL row": the version-pins row omits `github`, which the
  file header defines as a MANUAL row. Report's "builds from src-go render the same argv and
  chdir": reproduced above.

## Findings

### Low

1. **The milestone claim is already stale.** PLAN.md status, Task 2.1c and the journal say
   "M11 … in progress". md-viewer `main` now has `d541a567 Merge M11: speed fixtures, benchmarks and measurement` and `0cdff554 Mark M11 done` (pushed after `b2d1f942`). Not
   wrong at writing time, and the pin itself is a sound M10 tip, but the next PLAN.md touch
   should say M11 is merged and decide whether to move the pin.
2. **The host's `golang` must already be 1.26.9.** `play-golang.yml` installs `golang` with
   the default `state: present`, which does not upgrade an installed 1.26.8 (stable until
   2026-10-10). md-viewer's `go.mod` requires the newest patch, so a host that has not run
   `dnf upgrade` since then fails in `go` with "go.mod requires go >= 1.26.9 (running go
   1.26.8; GOTOOLCHAIN=local)". That is fail-fast and clear, so not blocking, but the
   meta-deploy comment could name it as the one precondition.

### Nits (no change needed to merge)

- The mimeapps key is set for `text/markdown` only; `text/x-markdown` is an alias of it in
  shared-mime-info so the lookup resolves, and the desktop file lists both.
- `-buildvcs=false` is dropped for the module form; correct, since a proxy build has no VCS.

## Not verified here

- The host run itself (Nautilus double-click, `--time`): Task 2.3, owner's.
