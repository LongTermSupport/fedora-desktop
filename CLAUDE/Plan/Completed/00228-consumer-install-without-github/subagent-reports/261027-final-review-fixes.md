# Plan 00228: final review fixes

Branch `fix/00228-review-findings`, commit deb484eac (not pushed). Fixes the findings of
[261027-final-qa-review.md](261027-final-qa-review.md). Each test was seen red first.

1. **Docs.** README.md and docs/installation.md say the password is asked once. installation.md
   describes ccc ("Claude" in the app grid, `ccc`, working in `~/CCC`, one sign-in). It says the
   speech-to-text Claude items are not included and say so when used, and that what is left out
   is ccy, podman-compose and the socket, not containers as a whole.
2. **Server profile (Task 2.8).** `consumer-install` asks `systemd.DefaultTarget` (new, in
   `internal/systemd`, which owns systemctl) at the end of its preflight. Anything but
   graphical.target is refused before host_vars is written. `play-AA-preflight-sanity.yml`
   asserts `install_audience != 'consumer' or provisioning_profile == 'desktop'`.
3. **Podman claim.** PLAN.md Task 3.1 and the checker comment are corrected. The checker
   looked only at `rpm -q podman-compose`, but the play installs it with pip `--user`. It now
   also checks `~/.local/bin/podman-compose`. The check is renamed
   `podman-developer-part-absent`, and there are still 17 checks.
4. **Empty vault (Task 2.9).** `vaultPresent` treats a zero-byte file as missing. The run
   makes it again, and `--optional-only` refuses it.
5. **Task 6.5 safe part.** `wsi-claude-process` checks for `claude` before it runs the token
   chooser. Without it, the run fails saying that post-processing needs the developer install
   and that plain dictation (Insert) works, and the panel shows that reason. The items are not
   hidden: clearing the keybindings would make the extension warn at every login, and hiding
   the menu items needs a new setting. `wsi-claude-token` is never reached there, so it is
   unchanged. The owner's decision stays open in Task 6.5.
6. **Success Criteria.** The five that the acfcb6814 VM run proved are ticked. Tasks 2.6-2.9
   are noted as proved by unit tests only.

**Host re-run needed:** yes. `00228-consumer-install-without-github` is queued in
`meta-deploy.list` (deploy.bash, then the three VM scenarios from the pushed HEAD), because the
installer's preflight, every play's preflight and the guest checker changed. The commit must be
pushed first.

`./scripts/qa-changed.bash` passed (go-qa diff mode, ansible-syntax, the vmtest manifest, the
stt suite, test-run-bash-consumer).
