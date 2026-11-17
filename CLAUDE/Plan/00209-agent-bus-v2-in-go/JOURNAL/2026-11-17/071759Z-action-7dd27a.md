## 07:17 · action · —

The archive cap file (CN5) now has a writer. `TestThePlayWritesTheCarryArchiveCap` and the
cap file's row in the derived-path test were red first: the play had no variable, no input
check and no task. play-teamchat.yml now derives `tc_carry_max_archive_bytes` from
`teamchat_carry_max_archive_bytes` (default 1073741824). The input check refuses any value
outside teamchat-admin's grammar (`^[1-9][0-9]{0,18}$`, at most int64's maximum), before
anything changes. "Write the Carry Archive Cap" writes it root's 0644 beside owner-group.
The meta-deploy queue's changed-plays entry describes the run that takes it to a host.
sc-D21 records the verb-by-verb check and the coordinator's condition: a future non-root
verb that writes a team's state must be judged by the carry gate.
