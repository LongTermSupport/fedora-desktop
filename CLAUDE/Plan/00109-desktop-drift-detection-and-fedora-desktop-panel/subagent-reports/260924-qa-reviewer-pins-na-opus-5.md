# qa-reviewer: 00109 DKMS pin not applicable (35b0d6e5), opus-5, 2026-09-24

Saved by the coordinator.

**Verdict: FIX-BEFORE-MERGE.**

## Should fix

1. **A desktop that ran the DisplayLink play and then lost DKMS entirely reported nothing.**
   The not-applicable test looked only at the DKMS state directory and the `dkms` command,
   not at the ledger. With the real manifest, no state directory, the command missing and
   `play-displaylink.yml` in the ledger, the new code returned no finding. The previous
   code returned "compared 0 of 1". The health probe is silent in that state too, so
   nothing reported it. Fix: not applicable only when the owning play never ran here, with
   an unreadable ledger treated as "may have run".
2. **Two comments the change contradicted:** an unreadable ledger keeps every pin
   applicable, and the registry never narrows the population.
3. **"The same two-part test as the health probe" was wrong.** The probe goes silent on an
   empty or absent state directory. The pin check needs it absent.

## Nits

- Dated history in `DESIGN-server-route.md` belongs in the journal.
- One docstring still said "answerable".

## Checked and clean

- **Coverage readers:** coverage is free text and `SCHEMA_VERSION` is unchanged. Only
  `acceptance.bash` reads it, and it handles all three sentence shapes.
- **Other DKMS states:** the state directory without the command, the command without the
  directory, other `dkms` failures, an unreadable directory and partial coverage all
  report.
- **Gates:** `qa-python`, `qa-version-pins` and all 2080 helper tests pass.

## Round 2, on 8228b0cc: PASS WITH NITS

All three findings fixed. With the real manifest, no state directory and `dkms` missing: an empty ledger (a server) gives no finding and "no tracked pin applies on this host"; a ledger recording `play-displaylink.yml`, or an unreadable one, gives the not-checked finding and "compared 0 of 1". The two new tests fail against 35b0d6e5. Nits: the design doc credited the ledger condition to the owner, since reworded to say the review narrowed the decision; the fifth VM run needs the push. 2082 helper tests OK.
