# Read-only commands for the bus host: the room's event shapes in the stall window

For the seat that holds the bus host's homeserver admin (Plan 00268 Task 1.3a). The window is
**2026-11-25 17:00:00Z to 17:15:00Z**, around the moment the busy team's Lightning stopped its
event loop (about 17:09:12Z, RESEARCH.md section 8). No member can read this: `teamchat history`
shows at most 100 recent items of one member's view (exit 64 above `--limit 100`).

## What it reads, and what it never prints

- **Route** (checked in this repository's code): the team's homeserver listens in plain HTTP on
  `127.0.0.1:<port>` (`host/render.go` `renderConfig`; the port is the `port` key of
  `/var/lib/teamchat-admin/<team>/config.toml`). The team's admin account
  (`@admin:<server_name>`, `ctl/ctl.go`) created and is joined to every team room, which keep
  `shared` history, so the standard Matrix client API `GET /rooms/{id}/messages` reads them. No
  `teamchat-admin` verb reads history, and Tuwunel's own admin interface is the admin room's
  commands, not a REST API; the client API is the read-only route.
- **The admin token** is read from `/var/lib/teamchat-ctl/<team>/admin.token` (`AdminTokenName`,
  `paths.CtlState`) by bash's own `$(< file)`, and handed to curl as a header file on a pipe
  (`-H @<(...)`): it never enters any process's argv, and is never printed.
- **The room id** comes from `/var/lib/teamchat-ctl/<team>/rooms.json` (keys `main`, `reports`,
  `questions`); the roles from the main room's `teamchat.roster` state event.
- **Bodies are dropped inside the same jq** that reads each page: the only fields kept are the
  event type, time, room purpose, the typed teamchat key, `msgtype`, the relation type, the
  thread root or replied-to id, content size in bytes, the sender as a role, and the redacted and
  edit flags. The raw page is never written to disk.
- Run it as root (both state directories are root-only). It sends GET requests only.
- If the team's homeserver runs inside an LXC guest, run it inside that guest (unchecked: the
  paths are the same there).

## The command

Replace `<team>` with the team id. The second argument is the room: run it for `main`, then
`questions`, then `reports`.

```bash
sudo bash -s -- '<team>' main <<'EOF'
set -euo pipefail
team=$1 key=$2
ctl=/var/lib/teamchat-ctl/$team
start=$(( $(date -ud '2026-11-25T17:00:00Z' +%s) * 1000 ))
end=$(( $(date -ud '2026-11-25T17:15:00Z' +%s) * 1000 ))
port=$(awk -F'= *' '/^port *=/{print $2; exit}' "/var/lib/teamchat-admin/$team/config.toml")
base="http://127.0.0.1:$port/_matrix/client/v3"
auth() { printf 'Authorization: Bearer %s\n' "$(< "$ctl/admin.token")"; }
uri() { jq -rn --arg v "$1" '$v|@uri'; }
room=$(jq -r --arg k "$key" '.[$k]' "$ctl/rooms.json")
main=$(jq -r '.main' "$ctl/rooms.json")
roster=$(curl -fsS -H @<(auth) "$base/rooms/$(uri "$main")/state/teamchat.roster/")
from='' rows=''
while :; do
  page=$(curl -fsS -H @<(auth) "$base/rooms/$(uri "$room")/messages?dir=b&limit=500${from:+&from=$(uri "$from")}")
  rows+=$(jq -c --arg purpose "$key" --argjson s "$start" --argjson e "$end" --argjson r "$roster" '
    .chunk[] | select(.origin_server_ts >= $s and .origin_server_ts < $e) | {
      ts: (.origin_server_ts / 1000 | todate), purpose: $purpose, type,
      typed: ([.content | keys[]? | select(startswith("teamchat."))] | join(",")),
      msgtype: (.content.msgtype // null),
      rel: (.content["m.relates_to"].rel_type
            // (if .content["m.relates_to"]["m.in_reply_to"] then "reply" else null end)),
      rel_to: (.content["m.relates_to"].event_id
               // .content["m.relates_to"]["m.in_reply_to"].event_id // null),
      bytes: (.content | tojson | utf8bytelength),
      role: (if (.sender | startswith("@admin:")) then "admin"
             elif ([.sender] | inside($r.humans)) then "human"
             else ($r.members[.sender].role // "other") end),
      redacted: (.unsigned.redacted_because != null),
      edit: (.content["m.relates_to"].rel_type == "m.replace"),
      id: .event_id }' <<<"$page")$'\n'
  oldest=$(jq '[.chunk[].origin_server_ts] | min // 0' <<<"$page")
  from=$(jq -r '.end // empty' <<<"$page")
  if [ -z "$from" ] || [ "$oldest" -lt "$start" ]; then break; fi
done
rows=$(grep -v '^$' <<<"$rows" | jq -sc 'sort_by(.ts) | .[]')
echo "== events, oldest first"; printf '%s\n' "$rows"
echo "== count per type, typed key and relation"
jq -sr 'group_by([.type, .typed, .rel]) | .[] | "\(length)\t\(.[0].type)\t\(.[0].typed)\t\(.[0].rel)"' <<<"$rows"
echo "== count per minute"
jq -sr 'group_by(.ts[0:16]) | .[] | "\(.[0].ts[0:16])Z\t\(length)"' <<<"$rows"
echo "== largest ten by bytes"
jq -sr 'sort_by(-.bytes) | .[0:10][] | "\(.bytes)\t\(.ts)\t\(.type)\t\(.typed)\t\(.rel)\t\(.role)"' <<<"$rows"
EOF
```

Notes:

- `dir=b` with no `from` starts at the room's newest event and pages back 500 at a time until it
  passes 17:00:00Z, so a busy room since 25 Nov costs several pages; the GETs are read-only.
- The `id` and `rel_to` columns are event ids: install-specific, kept in the seat's untracked
  output only. In a report, keep the counts and shapes and drop the ids.
- Read receipts, typing and presence are ephemeral and not in `/messages`: this read cannot show a
  receipt burst. The members' link logs and `teamchat status` counts are the nearest record of
  those.
- Unchecked until run: that Tuwunel accepts a `/messages` request with no `from` (allowed by the
  Matrix spec since v1.3), and the `humans`/`members` shape of the roster's content as the API
  returns it (`proto/roster.go` reads those keys).

## What to look for

Anything in 17:05:00Z-17:09:12Z that is rare in the rest of the window: an edit (`rel` =
`m.replace`, the coordinator's retitle), a redaction, a thread root or reply in a thread the human
had open, a large body (`bytes` near the 4,000/8,000/16,384 caps), a state event (roster, power
levels, member), or a burst in the per-minute counts.
