## 19:44 · action · — — Phase A: A.1-A.3 built, A.4-A.5 handed off

Tasks A.1 to A.3 are built and pushed (d7ce117bf, ccy 3.124.103).

- **A.1, red proofs.**
  - Semgrep `ccy-host-applies-checkout-config` was red on exactly two places:
    `lib/common.bash:425` (the mounts reader) and `lib/network-management.bash:941`
    (`ccy_compose_up`).
  - Go `project_checkout_config_pin` was red on `internal/sessions/restore.go:220` only.
  - Both are green after the fix, and `qa-patterns` passes all 591 files.
- **The sweep found three more instances of the class.** They were reported to the
  coordinator and are not fixed here:
  - `.claude/ccy/.last-launch.conf` is applied by a headless launch.
  - A host `cc` runs `.claude/settings.json` hooks in a checkout that a container also mounts.
  - Host git in a mounted checkout reads `.git/config`.
- **Departure from the first design.** The mounts validator moved from bash to Go, and the
  deny list grew. A directory above a denied path is now refused (`~/.config`, `~/.local`), so
  an existing mounts line of that kind will be refused until it names the folder inside.
- **Stopped at the context budget.** A.4, A.5 and the independent review are left;
  HANDOFF-phase-a.md lists them.
