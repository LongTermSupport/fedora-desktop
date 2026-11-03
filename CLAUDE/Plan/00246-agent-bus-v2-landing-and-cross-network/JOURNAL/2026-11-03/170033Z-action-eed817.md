## 17:00 · action · —   — S7b part three: the health tick reads the terminator unit

Task 8.8, slice S7b, third part: the health tick reads the terminator's unit (X18c).

Why: with the networks on a second unit, a stopped `teamchat-tls@<team>` cuts every member off while the homeserver still answers the tick's probe on loopback and reads as healthy.

Test first. `terminator_test.go` states six expectations over the health fakes (the unit fake answers a terminator unit apart from the homeserver's list of states): a stopped terminator is a problem on the record while the homeserver's probe is ok and nothing is restarted; a failed one is named as failed; one whose state cannot be read is a problem and does not fail the tick; an active one is no problem and is asked about once, by the team's own unit name; both units down are both on the record; and no terminator is judged while a restore is recorded (HB3, the restore stops units on purpose). Five were red before the code; the sixth, the restore one, is negative, so I removed the guard and it went red on its intended line.

The code is `(*Tick).terminator` in `health/probe.go`, called after the homeserver's step in `gather`. It only reads state and writes a problem: the unit restarts always (X18a), so one that is not active was stopped or failed past its start limit, which is for a person to see. A probe through the terminator (a TLS request to a listen address with the team CA), which would also catch a unit that is active and not answering, is not built; X18c and PLAN.md say so.

Verified: the health tests under `-race`, and `golangci-lint` clean over the package.
