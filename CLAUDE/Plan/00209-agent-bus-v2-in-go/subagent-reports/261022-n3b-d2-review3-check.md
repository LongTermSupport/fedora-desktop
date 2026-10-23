# n3b-D2 review 3: fix check

Plan 00209, slice n3b. Scope: `git diff d3c342e31..12f27da00` only (design-text fixes), against
F1-F9 of `261022-n3b-d2-review3.md`. Lines are at `12f27da00`. C is `DESIGN-v2-C-host.md`.

## Findings F1-F9

| ID  | Sev   | Result   | Where                                                                                                                                             |
| --- | ----- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1  | BLOCK | RESOLVED | C:1851-1858 (step 10), C:1772-1773; SPEC-n3:927 (n3-D12), :373-376 (LX9), :946 (n3b-D2); rework:179-183                                           |
| F2  | MAJOR | RESOLVED | SPEC-n3:947 (n3b-D3 row), :265-266 (LX2 points at it); rework:192-195                                                                             |
| F3  | MAJOR | RESOLVED | C:1761-1764 (step 03); SPEC-n3:288-290 (LX4, with test); rework:187-188                                                                           |
| F4  | MAJOR | RESOLVED | C:1930-1934 (absent phase); SPEC-n3:361-363 (LX8, with test); rework:189-192                                                                      |
| F5  | MINOR | RESOLVED | C:1927-1928; SPEC-n3:358 (LX8); rework:191                                                                                                        |
| F6  | MINOR | RESOLVED | C:1635 (names table), C:1725-1728 (step 01); SPEC-n3:251-253 (LX1)                                                                                |
| F7  | MINOR | RESOLVED | C:1779-1783 (names the entry without the record's handle; phase failure kept and stated, as a running container's wrong fact, also after a start) |
| F8  | MINOR | RESOLVED | C:1816-1819 (step 05: `gh`/`reach` offline per (user, team), others fail)                                                                         |
| F9  | MINOR | RESOLVED | rework:179-195 (record timing, `validate` reading every team's record, owner check, absent leaver, exit mapping)                                  |

F1 detail: step 10 now writes at the end of every phase, failed or not, and under each team's
lock once that team's step 06 joins return, before step 07, holding each joined handle as
`deferred`. The F1 scenario (B fails in c2 after A joined in c1, A then removed) now finds A's
record entry with its handle, so the absent phase parks it. n3-D12, LX9 (with the test) and the
n3b-D2 row say the same. The rework item matches.

## New contradictions this commit introduces (all MINOR)

- **N1. C:1935-1937, the absent phase's last sentence, is now stale.** It still says "as the
  present phase does after it succeeded (step 10)". Step 10 (C:1851) now writes after every
  phase, failed or not, so the cross-reference misstates it. This commit edited the paragraph
  just above it (C:1930-1934) and left this sentence. Fix: "as the present phase does at the end
  of every phase, failed or not (step 10)".
- **N2. SPEC-n3:927 (n3-D12) still says the absent phase "drops [a missing container's] record
  entry", with no user-move exception.** C:1927-1928 and LX8 (SPEC-n3:358) now rewrite a user
  move's entry instead (F5). The row was edited in this commit, and n3b-D2's list of amended
  items names n3-D12, but the exception is not stated there. Fix: add "(a user move's is
  rewritten for the new user, LX8)".
- **N3. C:1852 ("must never be unrecorded") claims more than the design gives, and C:1772-1773
  is garbled.** A crash after a join returns and before the step 06 write still leaves the handle
  unrecorded. C:1857-1858 says only "a crash inside the step 06 write itself", and C:1772 reads
  "a crash inside the step 06 record write, step 10", which runs two different writes together.
  The window is now one write wide, not a phase wide, but if the entry is removed after such a
  crash, nothing parks the handle. Fix: name the window as "between a join's return and the
  step 06 write", and drop ", step 10" at C:1772.

I found no contradiction with step 01, 03 or 05, the rest of the absent phase, LX1, LX2, LX4,
LX8, LX9 or the new n3b-D3 row. The status set in step 10 (`deferred`, `joined`, `offline`)
matches C:1976 and LX9. The `deferred` entry written mid-phase is the same (team, checkout,
seat) as its declared entry, so step 01's key rule, which now requires different tuples, never
refuses it.

## Verdict

MERGE. All of F1-F9 are RESOLVED (1 BLOCK, 3 MAJOR, 5 MINOR). The commit introduces 0 BLOCK,
0 MAJOR and 3 MINOR (N1-N3). These are wording drift, best fixed in the next text pass.
