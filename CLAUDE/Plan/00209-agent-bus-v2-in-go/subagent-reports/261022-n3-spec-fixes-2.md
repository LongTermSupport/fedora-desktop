# Fix log, round 2: SPEC-n3.md (review `261022-n3-spec-review-2.md`)

Each finding was checked against A 7.5, B 2.5, `proto/verdict.go` (`Lookup`, `Related`,
`humanAddressed`, `sentBySelf`), `seat/lockprobe.go`, `matrix/client.go` (`answered`, `once`),
`matrix/member.go` (`GetEvent`), PLAN.md lines 302-319, SPEC-n2c.md n2c-D14 and B 2.14 before any change.

- MAJOR 1, accepted (author): new SY4a, the production `proto.Lookup` (ledger and outbox, own sends recorded at send, a 1024-entry cache, then one bounded `GetEvent`; transient failure holds the batch; 404/403 unknown sender, uncounted); its tests; UH1 and gap 11 point at it; 2.14's count noted; n3-D20 added.
- MAJOR 2, accepted (author): LK1 tries every acquire, the seat lock's included, three times 50 ms apart (B 2.5 L1/L3), with the second-process `LOCK_SH` tests for `TakeSeat` and `guest remove`; LK3, GU3, GU5 and SA1 cite it.
- MINOR 1, applied: SY7 says the 45 s is per try (transport resend shares it, a 429 retry is a new try bounded by `maxTries`/`maxRateWait`); case 8 reworded.
- MINOR 2, applied (the seam option): gap 12 takes `once`'s per-try deadline from the client's existing clock seam; header line and SY7 case 3 cite it.
- MINOR 3, applied: TB2's sentence reworded to allow a grammar-checked pid or run id below a host-chosen directory opened by TB1; the five hostile rows added.
- MINOR 4, applied: LP1 names its inputs (LX3's rule, `/etc/passwd` by TB1) and adopts n2c-D14 (a refused open is free with no record), with tests.
- MINOR 5, applied: LX7 leaves a seat whose `place-bundle` was deferred out of that apply's desired seats and host copy, so LN8 releases the old identity's seat; test through step 08.
- MINOR 6, applied as a lead-in line naming each gap's slice (keeps the numbered list's IDs); GU1's VM-lab half moved to n3b; n3c's row says its VM run follows n3b's merge.
- MINOR 7, applied: LP2 owns the announcer adapter, the commit landing LP2 updates PLAN's `notice` line (PLAN.md not edited here), the wiring slice consumes it; godbus decided once in n3b for SU2 and the units seam (gap 13).
- MINOR 8, applied: LX4's `tmux` lookup treats `ELOOP` or non-regular per entry as "not here", a link-only `tmux` is a named miss, EL9-shaped fixture test.
- MINOR 9, applied: LX4's v1 holdback seam gets TB1's opener, not the root fd; FIFO test at the v1 env-file path.
- NIT 1, applied: section 11's T6/T7/T8 row split per test.
- NIT 2, applied: CL1 and gap 13 name two `keepout` rows.
- NIT 3, applied: n3-D1 says `place-bundle` gains exit 75 against B 2.13.
- NIT 4, applied: SY7 case 3 resumes from the marker alone (LXC `Bundle` has no `released_ms`).
- NIT 5, applied: section 11's T23 mapping adds LP1 and LP2.
