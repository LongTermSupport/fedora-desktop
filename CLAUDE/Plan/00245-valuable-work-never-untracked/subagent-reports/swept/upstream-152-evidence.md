Evidence from triaging the leftovers in the same project. 154 branches were left after the merged ones were deleted. Each was traced from its commits, plan state and saved agent replies:

| Why it was left                                                                                                                                     | Count |
| --------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| Merged (or contained in an integration branch under other hashes), never deleted                                                                    | 104   |
| Superseded by a redesign, never deleted                                                                                                             | 15    |
| Finished (review passed, report written), never merged                                                                                              | 14    |
| A subagent stopped midway (rate limit, context budget): last commit "WIP ... round 2 not yet run", dirty tree, or the whole index staged as deleted | 11    |
| Waiting on a human decision                                                                                                                         | 3     |
| Other (duplicate refs at the same tip, a branch waiting on a history rewrite)                                                                       | 7     |

Age since last commit: 124 had been idle 8-14 days, 27 more than 14, 3 less than 3 days.

What this says about the design:

- **The biggest cause is the missing last step, not blocking or crashes:** nothing deletes a branch once its work has landed. Item 5 of this request (lossless cleanup after a merge) would have removed about three quarters of the pile on its own.
- **Finished work silently left out of an integration branch is the costliest case.** An integration branch had been assembled from feature slices. One finished, reviewed slice (45 commits) was not in its ancestry. A second slice had 11 commits that existed only in one local clone, never pushed. A reviewed spec existed only locally. Nothing reported any of it. Requests:
  - **8.** At Stop and SubagentStop, refuse while the session holds commits on any branch that no remote contains (unpushed work). This is the stop guard of item 4, applied to every branch the session touched, not only the current one.
  - **9.** A dormancy report should name, for each idle branch, whether its commits are contained in the default branch or in any other remote branch. "Idle and contained" is cleanup. "Idle and contained nowhere" is lost work and should be loud.
- **Dormancy thresholds of 3 days (warn) and 7 days (block)** would have caught almost every item: none were 3-7 days old; they sat 8+ days because nothing said anything.

A related collision, which may want its own issue: the plan scaffolder takes plan numbers from a counter that is local to each clone and never pushed. A worker in a second clone of the same repository scaffolded a plan whose number a session in the first clone had claimed the same morning. The collision only appears when the branches meet. A shared source for the number would prevent it: the highest number on the remote default branch plus open remote branches, fetched before allocating, or a reservation ref pushed at scaffold time.
