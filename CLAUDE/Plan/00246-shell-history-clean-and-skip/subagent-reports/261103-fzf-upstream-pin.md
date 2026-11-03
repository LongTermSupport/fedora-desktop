# fzf from its upstream release

- Pin: `vars/fzf-pin.yml`, fzf 0.74.5 (junegunn/fzf latest), linux amd64 and arm64. The sha256 values
  match both the release's `fzf_0.74.5_checksums.txt` and the release API digest; the amd64
  tarball was downloaded and hashed to confirm.
- Install: `tasks/install-fzf.yml` puts the binary in `/opt/fzf-<version>/`, links
  `/usr/local/bin/fzf` (ahead of `/usr/bin` on the login PATH and sudo's secure_path), removes the
  Fedora package so only one fzf exists, checksum-verifies via `get_url`, asserts the version.
- Users: play-basic-configs, play-cli-tools, play-claude-yolo, `tasks/deploy-freeze.yml`
  (podfreeze, lxcfreeze). The ccy Dockerfile reads the same pin file (copied into the build context
  by `tasks/claude-yolo-image.yml`), sha256-checked, replacing the Debian package.
- Versions: container 2.79.22 to 2.79.23, CCY 3.108.0 to 3.108.1.
- Tracking: row in `vars/version-pins.yml` (installed resolver: `fzf --version`), routing entry.
- Footer: Ctrl+R hint moved from `--header` to `--footer`; test updated.
- Option audit (changelog 0.60 to 0.74.5): the gutter became a visible bar, so `gutter:-1` no longer
  hides it; `--gutter=' '` added to the Go `ui` picker and `ccy_tmux_pick`. No other used option
  was removed or changed (`--scheme=history`, `--read0`, `--tiebreak`, `--expect`, `--no-info`,
  `--header-first`, `--height=~N%`, `transform-query`, the alt-key binds are all current).
- Not run: ansible (container), qa-all. Ansible syntax checks fail in the worktree only because the
  vault password file is absent.
