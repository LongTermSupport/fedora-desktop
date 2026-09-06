"""Pure logic: which TCP ports does an `sshd -T` dump say sshd will listen on?

No I/O — `cli.py` runs `sshd -T` and feeds the text in here.

WHY THIS IS NOT A ONE-LINE GREP FOR `^port `. A port can be configured two ways,
and only one of them produces a `port` line:

    Port 2222                     ->  port 2222
    ListenAddress 0.0.0.0:2222    ->  port 22            <- the DEFAULT, still printed
                                      listenaddress 0.0.0.0:2222

So a machine that moved SSH using only `ListenAddress` reports `port 22` — and a
`^port `-only filter opens 22, the one port nothing is listening on, while the
real port stays shut. On a first-ever firewalld start over a remote SSH session
that is a lockout.

The `listenaddress` form is also where naive parsing goes wrong in the other
direction: a bare IPv6 address ends in `:<digits>` without carrying a port at
all (`2001:db8::1`), so "text after the last colon" is not a port. A port is
present only after a bracketed IPv6 literal (`[::]:2222`) or after a host
containing no colon (`0.0.0.0:2222`, `localhost:2222`).
"""

from __future__ import annotations

import re

# `sshd -T` lower-cases directive names, but nothing here depends on that.
_PORT_RE = re.compile(r"^port\s+(\d+)\s*$", re.IGNORECASE)

# host:port, where host is either a bracketed IPv6 literal or a colon-free host.
# Trailing content (e.g. `rdomain 1`) is permitted after the port.
_LISTEN_RE = re.compile(
    r"^listenaddress\s+(?:\[.*?\]|[^:\s]+):(\d+)(?:\s|$)",
    re.IGNORECASE,
)


def parse_ports(sshd_t_output: str) -> list[str]:
    """Every TCP port `sshd -T` output says sshd listens on.

    Order is the order encountered; duplicates collapse to their first
    occurrence so the caller gets a stable, idempotent list.
    """
    ports: list[str] = []
    for raw in sshd_t_output.splitlines():
        line = raw.strip()
        match = _PORT_RE.match(line) or _LISTEN_RE.match(line)
        if match is None:
            continue
        port = match.group(1)
        if port not in ports:
            ports.append(port)
    return ports
