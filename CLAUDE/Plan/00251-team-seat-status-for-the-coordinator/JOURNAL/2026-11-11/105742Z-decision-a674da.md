## 10:57 · decision · T1.3   — owner takes the defaults, Q1-Q8

The owner chose the defaults of RESEARCH-seat-status.md section 7, all eight questions:

- Q1: presence `status_msg` carries the seat state (design A; revises v2's A 6.6), subject to
  the Tuwunel presence probe of Task 1.4.
- Q2: a whole team idle with open requests wakes the coordinator and the humans, once.
- Q3: dwell times and thresholds are constants.
- Q4: delivery to a rate-limited seat is held until the reset.
- Q5: the coordinator skill requires pings for dispatch, so "acked, not done" is exact.
- Q6: all members see each other's state; only the coordinator is notified.
- Q7: the host aggregator (design C) waits for Plans 00224 and 00221.
- Q8: the token hot-swap proxy is host-local; a seat's local change to rate-limited is its
  trigger.

Next: Task 1.4's probes, then a spec.
