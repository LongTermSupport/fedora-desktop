# Install-path plan for X30: subagent report

Branch `docs/install-path-plan` (from `origin/spike/v2-cross-network`, b3397868e). Docs only: no
Go, Ansible or test change.

## Files

- `INSTALL-PATH.md` (new): the ordering table, the gates, who runs what, stages A, B and C,
  X30's proof list, what was checked, and four decisions.
- `PLAN.md`: a short "Install path (for X30)" section linking it. Plan 00209 and the spec are
  untouched.
- One journal entry (`action`) through `mkplan.bash --journal`.

## Findings grounded in the repository

1. `teamchat-tls@` has no text anywhere, as the coordinator said; `team apply` refuses at render
   without it and without `teamchat-server@` (`host/apply.go`, `host/tlsunit.go`).
2. Nothing installs `/usr/local/bin/teamchat` on the homeserver host, yet the terminator's
   drop-in runs it (`paths.MemberBinaryPath`); only the LXC guest kit makes that link, and design
   step 03 puts the host kit under `/usr/local/lib/teamchat/kit/`. Stage A's play has to make it.
3. The designed health unit (DESIGN-v2-C-host.md 3.11.1) cannot do S12 or S13: its
   `ReadWritePaths` leave out `/var/lib/teamchat-admin` (the TLS dir), and
   `IPAddressAllow=localhost` blocks S13's probe to the non-loopback listen addresses. No Go makes
   the health unit text, even on the S13 branch.
4. Firewall: rules are accepts in the zone of the routing interface, or the default zone. The LXC
   bridge is in `trusted` (accepts everything), and no play sets the default zone (a stock Fedora
   Workstation zone opens 1025 to 65535). So today the firewall may be no second wall.
5. The route check reads only the main table (`/proc/net/route`): a VPN in its own policy table
   is refused as default-routed.
6. The VM lab gives each guest its own passt user network. It has no multi-network guests, so
   the three-network scenario first needs a new lab capability.

## Decisions for the coordinator

The S13 health filter (recommended: a per-team drop-in), firewalld zones (recommended: `team apply` refuses a permissive zone), a policy-routed VPN, and whether X30 counts at seat level or
needs a ccy member. Detail is in INSTALL-PATH.md.
