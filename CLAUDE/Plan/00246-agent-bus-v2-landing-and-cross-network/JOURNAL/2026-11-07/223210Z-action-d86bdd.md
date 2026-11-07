## 22:32 · action · —

The install path for X30 is written as a plan for the coordinator's review, docs only:
INSTALL-PATH.md (three stages in the coordinator's order: base units and their play, the ccy kit,
the launcher; then X30's proof list) and a short section in PLAN.md linking it. Findings that
shape the build: no text of `teamchat-tls@` exists anywhere; nothing installs
`/usr/local/bin/teamchat` on the homeserver host, which the terminator's ExecStart names; the
designed health unit cannot write the TLS directory (S12) nor reach the listen addresses (S13);
the LXC bridge's trusted zone and an unset default zone leave the firewall no second wall; the
route check reads the main table only, so a policy-routed VPN is refused; the VM lab has no
multi-network guests yet. Four decisions are left to the coordinator in the file.
