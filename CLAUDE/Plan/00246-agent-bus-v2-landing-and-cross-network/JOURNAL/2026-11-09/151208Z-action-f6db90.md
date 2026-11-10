## 15:12 · action · 8.8   — VM lab verbs for X30 built under fakes (E67 to E73) and the host triage that checks the udp interface

The VM lab's side of X30 is built under fakes on branch `feat/vmtest-links`, stacked on stage A: SPEC-vmtest.md E67 to E73, each verb tests first, in five commits.

- `link-up` and `link-down` (E67, E71): a link is a directory with two loopback UDP ports and nothing libvirt knows.
- `member-up --link <link>:<a|b>` (E68), up to four: a udp NIC after the passt one, a MAC of 52:54:00 and three random bytes, the end and MAC recorded before the boot and dropped when the boot fails.
- `member-up --ram-mib` (E69) and the RAM check over every running lab guest (E70): the manifest's comment promised a sum the code never took; the class is a claim in prose, which no detector can express, so it is fixed by making the code do what the comment says.
- `member-address` and `member-ssh --as` (E72, E73).

What is unproven: all of it on a real libvirt. The `udp` interface spelling is the design's and unverified; `x30-triage.bash` (queued in meta-deploy.list, read-only, no guest defined) shows which spelling the host's virt-install takes, whether libvirt's schema accepts the domain, and the QEMU command line it becomes. If both spellings are refused, `member-up --link` needs another way to make the NIC (editing the printed XML), and that is this entry's follow-up.

Not built: the lab play's input for the subnets (D1), the guest fixtures and `x30-lab.bash`. The coordinator has not answered D1 to D8 on the design report; the verbs above do not depend on them.
