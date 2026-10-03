#!/usr/bin/env python3
"""List the globals a Wayland compositor advertises, using only the stdlib.

Connects to $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY, sends wl_display.get_registry and
wl_display.sync, prints every wl_registry.global event until the sync callback fires,
then disconnects. It binds nothing, creates no surface and reads no clipboard, so the
compositor sees one client connect and leave.

Output (stdout): one line per global, "name<TAB>interface<TAB>version", then a summary of
the clipboard-relevant interfaces. Diagnostics go to stderr. Exits non-zero on any
connection or protocol failure, printing the exact error.
"""

import os
import socket
import struct
import sys

CLIPBOARD_INTERFACES = (
    "wl_data_device_manager",
    "zwlr_data_control_manager_v1",
    "ext_data_control_manager_v1",
    "zwp_primary_selection_device_manager_v1",
    "gtk_primary_selection_device_manager",
)

DISPLAY_ID = 1
REGISTRY_ID = 2
CALLBACK_ID = 3


def socket_path() -> str:
    display = os.environ.get("WAYLAND_DISPLAY")
    if not display:
        sys.exit("WAYLAND_DISPLAY is not set")
    if display.startswith("/"):
        return display
    runtime = os.environ.get("XDG_RUNTIME_DIR")
    if not runtime:
        sys.exit("XDG_RUNTIME_DIR is not set")
    return os.path.join(runtime, display)


def request(object_id: int, opcode: int, payload: bytes) -> bytes:
    size = 8 + len(payload)
    return struct.pack("=II", object_id, (size << 16) | opcode) + payload


def read_string(body: bytes, offset: int) -> tuple[str, int]:
    (length,) = struct.unpack_from("=I", body, offset)
    offset += 4
    text = body[offset : offset + length - 1].decode("utf-8")
    offset += (length + 3) & ~3
    return text, offset


def recv_exact(sock: socket.socket, count: int) -> bytes:
    data = b""
    while len(data) < count:
        chunk = sock.recv(count - len(data))
        if not chunk:
            sys.exit("compositor closed the connection")
        data += chunk
    return data


def main() -> None:
    path = socket_path()
    print(f"socket: {path}", file=sys.stderr)
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(5)
    try:
        sock.connect(path)
    except OSError as error:
        sys.exit(f"connect failed: {error!r}")

    pid, uid, gid = struct.unpack(
        "=iII", sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12)
    )
    print(
        f"peer credentials as seen here: pid={pid} uid={uid} gid={gid}", file=sys.stderr
    )

    sock.sendall(
        request(
            DISPLAY_ID, 1, struct.pack("=I", REGISTRY_ID)
        )  # wl_display.get_registry
        + request(DISPLAY_ID, 0, struct.pack("=I", CALLBACK_ID))  # wl_display.sync
    )

    found: list[tuple[int, str, int]] = []
    while True:
        object_id, word = struct.unpack("=II", recv_exact(sock, 8))
        size, opcode = word >> 16, word & 0xFFFF
        body = recv_exact(sock, size - 8)
        if object_id == DISPLAY_ID and opcode == 0:  # wl_display.error
            bad_object, code = struct.unpack_from("=II", body, 0)
            message, _ = read_string(body, 8)
            sys.exit(f"wl_display.error object={bad_object} code={code}: {message}")
        if object_id == REGISTRY_ID and opcode == 0:  # wl_registry.global
            (name,) = struct.unpack_from("=I", body, 0)
            interface, offset = read_string(body, 4)
            (version,) = struct.unpack_from("=I", body, offset)
            found.append((name, interface, version))
        elif object_id == CALLBACK_ID and opcode == 0:  # wl_callback.done
            break
    sock.close()

    for name, interface, version in found:
        print(f"{name}\t{interface}\t{version}")
    advertised = {interface for _, interface, _ in found}
    print("--- clipboard-relevant ---")
    for interface in CLIPBOARD_INTERFACES:
        print(f"{interface}: {'PRESENT' if interface in advertised else 'absent'}")


if __name__ == "__main__":
    main()
