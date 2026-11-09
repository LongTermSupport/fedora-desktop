## 23:07 · action · #159   — The stale send stop of issue 159 fixed

Issue #159 built on `fix/recover-stale-401-159`: the sender's 401 or 403 stops a team's sends
only when the token the PUT was made under is the team's `sendsUnder`, which a recovery sets in
the same state-goroutine step as it clears the stop. A stale refusal stops nothing; the send is
tried again under the new token. Red before the fix for both codes
(`TestARefusalOfAnOldTokenDoesNotStopTheNewOne`). The class has no rule (GoProjectRules.md,
"Defect classes that cannot be a rule"); the sweep found the member's other answer-set refusals
sound. Report: `subagent-reports/261105-fix-sender-opus-report.md`.
