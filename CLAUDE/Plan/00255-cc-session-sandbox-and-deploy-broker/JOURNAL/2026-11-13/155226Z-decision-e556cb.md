## 15:52 · decision · —

Plan written for issue #101, the owner's choice of options 3 + 4.

Order chosen so the owner can run plays at every step: broker first, beside today's
passwordless sudo; then the seal, opt-in and then default; passwordless sudo removed last.

Findings that shaped it (detail in RESEARCH.md):

- The self-update runner (Plan 00137) already is most of a broker: root-owned clone, plays run
  as the user through runuser with the become password on an inherited descriptor, the shared
  play lock, ptrace_scope=1. run.bash's unattended path accepts exactly that caller.
- "A sha on origin" proves the commit is public and recorded, not that it is safe: agents can
  push. Hence D3 (owner's click for plays outside a short list).
- xdg-dbus-proxy filters by allowlist only; "refuse the secrets service, pass the rest" cannot
  be expressed. The allowlist also has to refuse services that start programs outside the seal.
- A seal alone does not stop root: sudo fails inside a user namespace, but tmux, the systemd
  user manager and the owner's shell start-up files run code unsealed. Hence D5 and the
  read-only start-up files, and passwordless sudo going last.
- The seal fits in cc-claude, which is already Go and is what cc starts.

Decision gates D1-D9 put to the owner with recommendations; triage.bash queued with --active.
