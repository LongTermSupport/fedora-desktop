## 10:00 · decision · —   — D1 decided: take Lightning's latest every run

The owner (2026-11-14): every issue we raised on Lightning has been fixed; pull the latest
changes and keep feeding back issues, since the maintainer is responsive. That settles D1: no
pin, take every release.

Upstream state checked with gh: pull request #77 (thread root grows then scrolls) merged; issues
#75, #76, #78 (text selection across messages) and #79 (thread list uses half its height)
closed. The latest release is still v0.10.5 (2026-11-08), which carries none of these, so Task
2.5 waits for the next release to reach Flathub.

Done: `tasks/flatpak-install.yml` gained `flatpak_update` (a `flatpak update` changed only when it
updated something; the module's `state: latest` reports changed on every run and would break a
changed=0 proof run), set for Lightning in `play-matrix-clients.yml`; `Mizerd/lightning` added
to the watch list; Task 2.7 opened as the standing duty to report what we find. Merged to F44 as
623d44395 after CI run 43624400239; a host run with `--tags lightning` is queued in
meta-deploy.list.

Faults met on the way, filed as fedora-desktop#294: the ansible_enforcement hook denies a search
or a comment that names `flatpak update`, and lint_on_edit fails every Ansible edit in a fresh
worktree for want of the vault password file.
