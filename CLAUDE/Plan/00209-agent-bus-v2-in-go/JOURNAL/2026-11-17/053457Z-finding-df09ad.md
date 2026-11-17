## 05:34 · finding · —

Item 15 (SPEC-carry section 13), red. admin/wire: TestCarryEngineStagesAsTheHost (the import's stage is carry.StageOS under the root with the teamchat-ctl uid and gid and the System's owner group; the drill's restart is host.Serve) and TestCarryNeedsTheCtlAccount. Seen red: "building the carry engine looked the account up 2 times, want 1": machine.carry looked the teamchat-ctl account up twice (ctlUID, then ctlIDs) and built the engine before checking the second lookup's error. Every other seam and guard (SG2, SG3, SG4) was already wired and tested.
