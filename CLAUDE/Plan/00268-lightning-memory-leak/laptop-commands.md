# Read-only commands for the laptop's host seat

For the coordinator to pass to the laptop's host seat. Every command here only reads. None
stops, restarts, signals, attaches to or reconfigures Lightning or anything else. Nothing needs
sudo. Keep the raw output in the seat's own untracked scratch. The output holds team names,
paths under the home directory and room ids, so it is anonymised before anything reaches a
tracked file in this repository: "the busy team", "a quieter team".

Never quote message content, access tokens, recovery keys or database contents. The commands
below never open Lightning's databases or its keyring.

`APP=org.lightning_matrix.Lightning` in every block.

## 1. Which build, and when it changed

```bash
APP=org.lightning_matrix.Lightning
flatpak info "$APP" | cat
flatpak history --columns=time,change,application,branch,commit --no-pager 2>&1 | grep -F "$APP" | cat
```

Answers: the version running now, and whether a Flatpak update fell between the instance's start
on 25 Nov and the kill at 18:08:56Z (0.10.7 was published on GitHub on 2026-11-21).

**Before `lightning_diagnostics` is set (Task 1.4):** the `Version:` line of `flatpak info` must
read 0.10.7 or later. Only 0.10.7 caps `--log-file` (32 MiB, rolled once: 64 MiB at most); 0.10.6
writes it without a cap (read in Lightning's source at both tags, not run). Unchecked so far.
Also read-only, for the running build (it names no team):

```bash
APP=org.lightning_matrix.Lightning
flatpak ps --columns=instance,application,commit | grep -F "$APP" | cat
flatpak info --show-commit "$APP" | cat
```

The running instances' commit should equal the installed one; one that differs was started
before an update and runs the older build until it is opened again.

## 2. Every Lightning process and its scope

```bash
ps -eo pid,ppid,rss,vsz,etimes,nlwp,pcpu,cputimes,comm --sort=-rss | grep -E '[l]ightning|[b]wrap' | cat
for p in $(pgrep -f '[l]ightning-matrix'); do
  echo "== $p"
  grep -E '^(VmRSS|VmHWM|RssAnon|RssFile|RssShmem|VmSwap|Threads):' "/proc/$p/status"
  cat "/proc/$p/cgroup"
  tr '\0' '\n' < "/proc/$p/environ" | grep -E '^XDG_CONFIG_HOME=' | awk -F/ '{print "team dir: " $(NF-1)}'
  ls "/proc/$p/fd" | wc -l
done
```

Answers: which process holds the memory (the main `lightning-matrix` process or another), its
thread count, CPU time (a busy loop shows as high `cputimes` against `etimes`), open file
descriptors, and which team each instance is (the team directory name; anonymise it).

## 3. The scope's memory by class

For each scope path printed in step 2 (`0::/user.slice/...`):

```bash
CG=/sys/fs/cgroup/<the path from /proc/<pid>/cgroup>
cat "$CG/memory.current" "$CG/memory.peak" "$CG/memory.swap.current" 2>&1
grep -E '^(anon|file|shmem|file_mapped|kernel|kernel_stack|pagetables|sock|slab|anon_thp) ' "$CG/memory.stat"
systemd-cgtop --order=memory -b -n 1 --depth=5 user.slice | cat
```

Answers: anon heap against shared memory (`shmem`, graphics buffers) against file cache. Mostly
`anon` points at the heap (hypotheses H1-H6 in RESEARCH.md); mostly `shmem` at graphics (H7).

## 4. Where in the process the memory is

For the largest Lightning process `<pid>`, at the first and last sample of step 6:

```bash
cat /proc/<pid>/smaps_rollup
awk '/^[0-9a-f]+-[0-9a-f]+ /{name=$6; if(name=="")name="[anon]"} /^Rss:/{rss[name]+=$2; n[name]++} END{for(k in rss) printf "%10d kB %5d maps  %s\n", rss[k], n[k], k}' /proc/<pid>/smaps | sort -rn | awk 'NR<=15'
awk '/^[0-9a-f]+-[0-9a-f]+ /{range=$1; name=$6} /^Rss:/{if($2>=65536) printf "%10d kB %s %s\n", $2, range, name}' /proc/<pid>/smaps | sort -rn | awk 'NR<=20'
grep -h -E '^drm-(memory|resident|total|shared)' /proc/<pid>/fdinfo/* 2>/dev/null | sort | uniq -c | cat
```

Answers: `[heap]` (the GUI thread's main arena) against many anonymous mappings near 64 MiB
(glibc per-thread arenas: Rust/tokio worker threads) against `/memfd:` or `/dev/dri` (graphics),
and GPU memory the driver reports.

## 5. Which thread is busy

```bash
ps -L -o tid,pcpu,cputime,comm -p <pid> --sort=-pcpu | awk 'NR<=15'
```

Answers: the GUI thread, a QML render thread, or a tokio worker: the owner of a loop (H1).

## 6. Growth rate per instance

A bounded loop, run in the background, one line per instance every 5 minutes for 60 minutes:

```bash
for i in $(seq 0 12); do
  for p in $(pgrep -f '[l]ightning-matrix'); do
    printf '%s pid=%s ' "$(date -u +%FT%TZ)" "$p"
    awk '/^Pss:/{pss=$2} END{printf "pss_kb=%s ", pss}' "/proc/$p/smaps_rollup"
    grep -E '^(VmRSS|RssAnon|RssShmem|VmSwap):' "/proc/$p/status" | tr -s ' \n' ' '
    awk '{printf "utime=%s stime=%s\n", $14, $15}' "/proc/$p/stat"
  done
  sleep 300
done
```

Answers: growth in MiB a minute for the busy and the quieter instance, and CPU ticks used in each
interval. Note beside each sample, without changing anything, whether the busy instance's window
is focused, which room is open and whether a thread panel is open, if the human at the laptop
can say.

## 7. The killed instance's last hours

```bash
journalctl --user --since '2026-11-25 11:00:00 UTC' --until '2026-11-25 18:10:00 UTC' --no-pager -o short-iso \
  | grep -i -E 'lightning|flatpak' | awk '{$1=""; $2=""; print}' | cut -c1-160 \
  | sort | uniq -c | sort -rn | awk 'NR<=40'
journalctl -b -t systemd-oomd --no-pager -o short-iso | cat
```

Answers: repeated lines with their counts (a loop often logs), truncated to 160 characters so no
message body is quoted whole.

## 8. Lightning's own logs and data sizes

```bash
APP=org.lightning_matrix.Lightning
du -sh ~/.var/app/$APP/teams/*/data ~/.var/app/$APP/teams/*/config ~/.var/app/$APP/teams/*/cache 2>&1 | cat
for t in ~/.var/app/$APP/teams/*/; do
  echo "== $(basename "$t")"
  find "$t" -type f -printf '%s %TY-%Tm-%Td %TH:%TM %P\n' | sort -rn | awk 'NR<=10'
done
find ~/.local/state -maxdepth 3 -name 'lightning-*.log' -printf '%s %TY-%Tm-%Td %TH:%TM %p\n' | cat
```

Answers: the size of each instance's SDK stores (`matrix-sdk-event-cache.sqlite3` and its `-wal`
grow with events kept), and any log file Lightning or ccy-teams wrote. For a log file found, count
repeated lines the same way as step 7, never quoting bodies:

```bash
awk '{s=substr($0,1,120); gsub(/[0-9]+/,"N",s); print s}' <log file> | sort | uniq -c | sort -rn | awk 'NR<=30'
```

## 9. The busy team's traffic shape (if the seat can read it without a token in the output)

```bash
teamchat status --team <busy team> | cat
teamchat history --team <busy team> | cat
```

Answers: link restarts, drop counts and the last items (kinds, senders, times); counts only go
into a report. The full day's event counts by type and room need a read-only probe this plan
builds (Task 1.3), not a command run by hand.

## 10. What the OOM incident left behind (owner question Q20)

The killed instance started about 25 Nov 11:00Z and died at 18:08:56Z.

```bash
# systemd's own accounting when the scope stopped: CPU time used and memory peak.
journalctl --user -b --no-pager -o short-iso --since '2026-11-25 10:00:00 UTC' --until '2026-11-25 18:20:00 UTC' \
  | grep -F 'org.lightning_matrix.Lightning' | grep -i -E 'consumed|memory peak|oom|killed|deactivated|failed' | cut -c1-240 | cat
journalctl -b --no-pager -o short-iso -u systemd-oomd | cat
```

A steady, high CPU time over the instance's life points at a loop (H1); little CPU at a large
peak points at accumulation (H2, H3).

```bash
# Which journal identifiers Lightning writes under, if it was opened from the app grid.
journalctl --user -b -F SYSLOG_IDENTIFIER | grep -i -E 'lightning|flatpak' | cat
# Its memory readings from the trim, and its stall and repeat summaries, in that window.
journalctl --user -b --no-pager -o short-iso --since '2026-11-25 10:00:00 UTC' --until '2026-11-25 18:10:00 UTC' \
  | grep -E 'idle trim:|GUI stall|repeated|scroll-memory' | cut -c1-200 | cat
```

```bash
# The ccy-teams launch log, if ccy-teams opened it: sizes first, then the same readings.
find ~/.local/state -maxdepth 4 -name 'lightning-*.log' -printf '%s %TY-%Tm-%TdT%TH:%TM %p\n' | cat
grep -h -E 'idle trim:|GUI stall|repeated|scroll-memory|panic|abort' <that log> | cut -c1-200 | cat
awk '{s=substr($0,1,100); gsub(/[0-9]+/,"N",s); print s}' <that log> | sort | uniq -c | sort -rn | awk 'NR<=30'
```

```bash
# Lightning's own rolling log, only if "Keep a diagnostic log" was on, and the event store's size.
APP=org.lightning_matrix.Lightning
grep -r -l -F 'keepLog=true' ~/.var/app/$APP/teams/*/config 2>&1 | cat
find ~/.var/app/$APP/teams -type d -name logs -printf '%p\n' -exec ls -la {} \; | cat
find ~/.var/app/$APP/teams -name 'matrix-sdk-event-cache.sqlite3*' -printf '%s %TY-%Tm-%TdT%TH:%TM %P\n' | cat
```

The output names teams and paths: anonymise before it reaches any tracked file. The `idle trim:`
lines carry only numbers; the repeated-line counts carry no message text after the 100-character
cut, but check before quoting.

## Not read-only: needs the owner's yes and goes through IaC

Running the busy team's next instance with Lightning's traces on (`--log-file <path>`,
`LIGHTNING_SCROLL_MEMORY_TRACE=1`, `LIGHTNING_SYNC_TRACE=100`) changes how it is launched. That
is the `lightning_diagnostics` key of the team's host_vars entry: the launcher
`agent-bus-lightning` writes (Task 1.4) and the watchdog service (Task 1.7), deployed by the play
through `meta-deploy.bash`, never typed on the laptop.
