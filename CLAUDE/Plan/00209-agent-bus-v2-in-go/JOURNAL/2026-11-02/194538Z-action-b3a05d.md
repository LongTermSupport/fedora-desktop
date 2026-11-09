## 19:45 · action · —   — Task 8.6: the lines the merge lost are back

Task 8.6 done: the lines the merge dropped are back, merged by meaning.

- `CLAUDE/QA.md`: the `qa-go-qa.bash` row names the busfailpoints stages and the `project-rules-tagged` stage again.
- `CLAUDE/GoProjectRules.md`: the `project_lifecycle_turn`, `project_keeper_log_barrier` and `project_agent_branch` sections, the table rows for them and for `fakeclock_count`, `goroutine_end`, `rawconn_probe` and `leakcheck_once`, and the two bullets of the "cannot be a rule" list (a read of state a live goroutine may change; a fake that announces before it acts). Pure insertions.
- `src-go/CLAUDE.md`: the project-rules sentence names teamchat's bounds on its injected clock and its agent seam.
- Plan 00194 Tasks 2.0e and 2.0g, and Plan 00209's INT3 and `n2f-D12` rows, as feat/bus-v2-n3e had them.

Left out on purpose: v2's teamchat-scoped text of `project_realtime_bound`. F44's module-wide rule of the same name replaced it, and its fixtures (`fix/internal/realtime`, `fix/internal/realclock`) cover the same cases, so the four `teamchat/realtime` fixture files and the `teamchat/realclock` one, which no test names, are removed.

Plan 00209's PLAN.md is far past the plan-size block limit (the daemon's plan QA now blocks edits to it). It carries a `MUST_EXCEED_PLAN_SIZE_BECAUSE` declaration with the reason; whether to split the plan instead is a question for the owner.

The spike's first CI run had named the work list; the merge of F44 also brought F44's new `project_liveness_word` rule, which flags nine v2 sites that were not in the Phase 8 list. They are fixed as Task 8.1's class work.
