# qa-reviewer: Plan 00134 T3.1 and T3.2 (7e76ae1d), opus-5, 2026-09-24

Saved by the coordinator.

**Verdict: PASS WITH NITS.** Nothing blocking.

## Nits, and what became of them

1. **The docs and the launch message say the unrelabelled mounts are unreadable.** On a
   Permissive host they are readable, and each access is logged. Fixed after the merge in
   `docs/ccy.md` and the launch message, within the still-undeployed 3.65.0.
2. **The comment said `check_mode: false` only reads.** Under `--check`, `docker info` can
   start a stopped dockerd through the socket that `play-docker.yml` enables. The comment
   now says so.
3. **With dockerd installed but not running, the play stops on docker's own error, not the
   assert's.** That is the correct stop, and the play's header already requires it. A
   clearer message would help. Accepted.
4. **Human output is parsed with a regex.** A `--format` query would be sturdier; the field
   name is unconfirmed. Accepted: the regex was run through Ansible 2.19.13's own
   templating against eight shapes.
5. **No journal record of the red-then-green run.** The reviewer reproduced the red half:
   the parent's function returns `off` for all three new Permissive cases.

Pre-existing: the `stat` task that sets `docker_daemon_check` has no tag, so
`--tags lxc_iptables` fails loudly on an undefined variable.

## Checked and clean

- **Every reader of the verdict** tests `!= off`, so `permissive` gets the same `:z`, `Z`
  and key staging as `enforcing`.
- **Private `Z` with concurrent sessions is safe:** the config dir and the key staging dir
  are both created fresh per session.
- **Tests:** 16 of 16 pass, against the function the launcher calls.
- **The docker assert** passes for `iptables+firewalld`, `iptables` and output with an extra
  sub-line. It fails for `nftables`, `nftables+firewalld`, `iptables-legacy`, a missing line
  and empty output.
- **Versions:** 3.65.0 and common 1.5.2 agree with the changelog and docs.
- **Placement:** an edit to the play that owns the assumption.
- **Public-repo safety:** no install-specific names.
