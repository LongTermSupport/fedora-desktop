## 06:36 · action · #278 — play-teamchat.yml forgotten in the host ledger

The play ledger on the desktop host was edited by hand, once, at the coordinator's request.
This followed issue #278, which landed on F44 at 27b4bad85.

Edit: `play-teamchat.yml`'s failed full run was forgotten. Its last full run had not succeeded,
so `changed_plays` kept listing it as RUN ("did not succeed").

Run in the checkout at F44 27b4bad85:

```text
$ python3 -m helpers.play_ledger.check_freshness --forget-play playbooks/imports/play-teamchat.yml
play-ledger: forgot the runs of playbooks/imports/play-teamchat.yml up to now; it is not judged here until it runs again.
exit 0
```

The next read, `python3 -m helpers.play_ledger.changed_plays`, exited 0. Its 20 lines do not
name `play-teamchat.yml`.

The play is next judged when it runs again.
