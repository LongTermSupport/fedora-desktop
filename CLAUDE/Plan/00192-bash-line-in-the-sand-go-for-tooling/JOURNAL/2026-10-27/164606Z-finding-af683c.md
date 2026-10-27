## 16:46 · finding · —   — reclaim host checks H1-H4 pass on 2.0.0; H5 the owner's

Host agent ran the reclaim host checks (SPEC-reclaim.md section 7) on 2.0.0 after the
changed-plays run. H1 to H4 pass. H5 not run: it needs baobab opened on the owner's desktop
and a screenshot, which the host agent may not do; it is the owner's.

H1: an ELF, `reclaim 2.0.0`, all four sections with real figures, exit 1 naming what it
could not read. Two findings: (a) stderr was about 95 raw `du: cannot read directory ...:
Permission denied` lines, 11 under /var/log and 83 under /var/cache; (b) `podman system df`
exited 125 on `open .../volumes/<a volume>/_data: permission denied` (a database image's
volume, its data owned by a container's uid), so the section said
`(podman storage unavailable)` on every run on that host.
H2: ncdu, the baobab question answered no, file listing, redraw, three invalid choices exit 1,
Ctrl-D exit 0, Ctrl-C exit 130 with `Cancelled.`.
H3: options 3, 4 and 5 previewed and answered no; `dnf history` and journal usage unchanged.
B12's "no sudo question" is trivially true there: the host user's sudo needs no password, so
it was not exercised.
H4: under a scratch HOME, 8 and 9 answered yes emptied the scratch .cache and Trash and left
the directories. Option 6's podman half read the scratch home's (empty) storage, so its
preview was not seen against real data; docker's half read the real rootful daemon, no pruned.
