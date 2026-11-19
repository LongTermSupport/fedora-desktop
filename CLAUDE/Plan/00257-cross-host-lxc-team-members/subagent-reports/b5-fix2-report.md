# B.5 second fix round: the terminator's slots, and the review of b5-fix-review.md

In progress. Each section is filled as its commit lands.

## Step 0: 1024 slots, 256 per source (review item 5)

The coordinator's ruling (c): `tlsterm.DefaultMaxConns` is 1024 and `DefaultMaxPerSource` stays
256 (`paths.MaxConnectionsPerSource`), so by default one source holds a quarter of the slots and
SEC-4 holds without any team setting. The terminator unit's `LimitNOFILE` is `2*1024+64`.

The homeserver's room for 1024 upstream connections, established from the source of the pinned
release (Tuwunel v1.9.4, `files/usr/local/share/teamchat/tuwunel.pin`):

- `src/main/server.rs:61` calls `sys::maximize_fd_limit()` at start, and fails the start if it
  cannot (`.expect`).
- `src/core/utils/sys/limits.rs:33-45` raises the soft `RLIMIT_NOFILE` to the hard limit.
- `teamchat-server@.service` sets no `LimitNOFILE`, so it gets systemd's default for system
  services, `DefaultLimitNOFILE=1024:524288` (systemd 240 and later, `systemd-system.conf(5)`):
  the homeserver runs with a soft limit of 524288, far above 1024 connections plus its database
  files. A host whose `system.conf` lowers the hard limit would change this; nothing in this
  repository does.
