## 00:39 · decision · Plan 00257 B.12

R9 is superseded by Plan 00257 Task B.12, the owner's order that an LXC member's claude
session never starts at boot. The member unit has Restart=no and no [Install] section,
nothing enables it, and it starts only on demand. Whether the container starts at boot is
up to its owner and is no longer required.

The R9 leg (`check_lxc_members` in \_acceptance-robust.inc.bash) therefore now checks the new
expectations for each online declared service: true unit: it is not enabled, nothing in
/etc/systemd/system/\*.wants links to it, and it has Restart=no. It no longer requires the
unit to be active, Restart=always or enabled, and it no longer reads lxc.start.auto. The
judgement is the pure function `r9_unit_judged`. It was proved with a scratch harness:
written first with the old rule (red on the new cases), then changed to the new rule (green).
ACCEPTANCE.md's R9 row and PLAN.md's description are updated to match.
