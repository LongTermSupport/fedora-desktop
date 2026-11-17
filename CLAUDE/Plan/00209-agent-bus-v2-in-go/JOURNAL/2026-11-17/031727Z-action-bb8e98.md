## 03:17 · action · —

Item 10b of SPEC-carry (#324), green: `carry.Import` and `team import`.

- carry/importrun.go: Import (archive cap, lock wait 0, SG5's importGuard), fresh (validate,
  record at staging, stage, --sha256 compare, record at staged, place), SI8 staging through
  ReadArchive into HomeserverCarryStaging and CarryStaging (with carry.json, the manifest copy),
  SI9 placement in PlacedRoots order (placed appended first; carry.place with the staged files and
  the lost exports streamed by StagedLost; approved.json saved; carried.json; SI7 again; the
  commit rename; step 6), SI10's --resume (staging: clean and start again; staged: re-check the
  staged tree; placing: the rename rule and a redo of a last ctl or status root) and --discard,
  SI11's answer.
- carry/stage.go: the Stage seam and StageOS (no link followed anywhere; renameat2
  RENAME_NOREPLACE; fd-relative removal via restore.RemoveAt; host.MkdirOwned, MkdirStatus,
  FileApprovals with the owner group).
- admin: Carrier.Import, CarryEngine.Import, teamImport with carryFlags (--sha256 64 hex,
  --resume with --discard, both 64), registered; wire gives the carry deps a StageOS.
- SPEC-carry: SI9 step 6 and sc-D12; PLAN.md's carry line.

Green: go test ./internal/teamchat/carry/ ./internal/teamchat/admin/... restore host pass; the
kill test runs every call of a sealed and a drill import, each followed by --resume and by
--discard.
