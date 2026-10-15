# md-viewer install play (Task 2.2): report

## What was written

- `playbooks/imports/optional/common/play-md-viewer.yml`, an optional `scope: gnome` play. It
  imports `play-golang.yml`, installs Gio's build and runtime libraries with dnf, and builds
  md-viewer at the pinned commit through `tasks/build-go-command.yml`. It then asserts that
  `md-viewer --version` names that commit, deploys `~/.local/share/applications/md-viewer.desktop`
  (checked with `desktop-file-validate`) and sets `text/markdown=md-viewer.desktop` in
  `~/.config/mimeapps.list`.
- `tasks/build-go-command.yml` takes a new optional input, `go_command_module`
  (`<package>@<version>`). With it, the build is `go install -trimpath` from the Go module proxy
  into the same scratch directory, run outside `src-go`, and the existing checksum-compared copy
  installs it. An assert refuses `@latest`, an unpinned value, and a package whose last element
  is not `go_command_name`. Builds from `src-go` render the same argv and chdir as before.
  This was checked by rendering both forms with Ansible's Templar.
- `vars/version-pins.yml` gets a MANUAL row for `md_viewer_version`, because md-viewer has no
  tags.
- `docs/playbooks.md` gets an entry. `CLAUDE/Plan/meta-deploy.list` queues the play.

## Pin

`md_viewer_version: b2d1f942fcdd8508f93a5ca40807a387032b02ae`. This is the tip of md-viewer
`main` ("Mark M10 done"), after the M10 merge. The repository has no tags.

## Facts checked

- md-viewer's git log has the M0 to M10 merges on `main`. M11 is unticked in its
  `docs/MILESTONES.md`.
- Fedora 44's golang is 1.26.9 (mdapi, f44 and f44-updates). That is what md-viewer's `go.mod`
  needs under `GOTOOLCHAIN=local`.
- The `#cgo linux pkg-config:` lines in Gio v0.10.3 name `x11 xkbcommon xkbcommon-x11 x11-xcb xcursor xfixes egl wayland-egl wayland-client wayland-cursor`, and Gio's sources include
  `vulkan/vulkan.h`. Each package the play installs provides a group of these on F44 (mdapi
  `provides`). md-viewer's README names `mesa-libGLES-devel` for Fedora, but on F44 that name
  exists only as a virtual provide of `libglvnd-devel`. The play installs `libglvnd-devel`,
  which provides `pkgconfig(egl)`.
- `go install …/cmd/md-viewer@<hash>` resolves through the proxy with `GOFLAGS=-mod=readonly`.
  In the container it fails only at `pkg-config`, as expected without the libraries. The
  pseudo-version is `v0.0.0-20261015155740-b2d1f942fcdd`, and `--version` prints it.
- `ansible-playbook --syntax-check` passes for the new play and for `play-ftp-camera.yml` and
  `play-claude-yolo.yml`, which import the changed task file.

## Not done here

- The host run, the Nautilus double-click check and the `--time` measurement belong to the
  owner (Task 2.3).
- Owner decisions 2.1b (emoji fonts, dark mode) are still pending. The desktop file starts
  md-viewer without `--dark`.
- Every play that imports `build-go-command.yml` (claude-yolo, ftp-camera, vm-test-lab, the
  freeze tasks) now has a changed dependency. Their behaviour is unchanged, so none is queued.
