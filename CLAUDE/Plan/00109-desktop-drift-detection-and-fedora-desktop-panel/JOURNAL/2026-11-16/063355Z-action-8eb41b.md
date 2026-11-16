## 06:33 · action · — — play-teamchat.yml forgotten on the desktop host (#278)

The host agent ran the one-off forget of play-teamchat.yml on the desktop host, in a checkout at F44 27b4bad85 (issue #278 landed). It clears the failed narrowed run that, before #278, had made the play due for ever.

Command:

    cd <checkout> && python3 -m helpers.play_ledger.check_freshness --forget-play playbooks/imports/play-teamchat.yml

Output, exit 0:

    play-ledger: forgot the runs of playbooks/imports/play-teamchat.yml up to now; it is not judged here until it runs again.

Next ledger read: `python3 -m helpers.play_ledger.changed_plays` exited 0, and none of its 20 lines names play-teamchat.yml (no RUN and no did-not-succeed entry). The play is judged again from its next full run.
