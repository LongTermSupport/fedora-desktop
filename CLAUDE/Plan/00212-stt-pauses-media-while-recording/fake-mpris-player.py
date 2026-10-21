#!/usr/bin/python3
"""Plan 00212 acceptance fixture: one fake MPRIS player on the session bus.

    fake-mpris-player.py NAME STATUS LOG

Owns org.mpris.MediaPlayer2.NAME and answers the Player interface's Pause and Play and its
PlaybackStatus and CanPause (true) properties, starting at STATUS. A second interface,
org.fedoradesktop.Test.Player, has SetStatus(s), which stands for the owner changing the
player by hand. Each method call appends "<method> <status after it>" to LOG. Runs until
killed; exit 1 if the name cannot be owned. Only ever run on the private bus
check-media-pause.bash starts, so no real player is touched.
"""

import sys

from gi.repository import Gio, GLib

PATH = "/org/mpris/MediaPlayer2"
XML = """
<node>
  <interface name="org.mpris.MediaPlayer2.Player">
    <method name="Pause"/>
    <method name="Play"/>
    <property name="PlaybackStatus" type="s" access="read"/>
    <property name="CanPause" type="b" access="read"/>
  </interface>
  <interface name="org.fedoradesktop.Test.Player">
    <method name="SetStatus">
      <arg name="status" type="s" direction="in"/>
    </method>
  </interface>
</node>
"""


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: fake-mpris-player.py NAME STATUS LOG", file=sys.stderr)
        return 64
    name, status, log_path = sys.argv[1:]
    state = {"status": status, "rc": 0}
    node = Gio.DBusNodeInfo.new_for_xml(XML)
    loop = GLib.MainLoop()

    def on_call(_conn, _sender, _path, _iface, method, params, invocation):
        if method == "Pause":
            state["status"] = "Paused"
        elif method == "Play":
            state["status"] = "Playing"
        else:
            state["status"] = params.unpack()[0]
        with open(log_path, "a", encoding="utf-8") as log:
            log.write(f"{method} {state['status']}\n")
        invocation.return_value(None)

    def on_get(_conn, _sender, _path, _iface, prop):
        if prop == "PlaybackStatus":
            return GLib.Variant("s", state["status"])
        return GLib.Variant("b", True)

    def on_bus(conn, _name):
        for iface in node.interfaces:
            conn.register_object(PATH, iface, on_call, on_get, None)

    def on_lost(_conn, lost):
        print(f"fake-mpris-player: cannot own {lost}", file=sys.stderr)
        state["rc"] = 1
        loop.quit()

    Gio.bus_own_name(
        Gio.BusType.SESSION,
        "org.mpris.MediaPlayer2." + name,
        Gio.BusNameOwnerFlags.NONE,
        on_bus,
        None,
        on_lost,
    )
    loop.run()
    return state["rc"]


if __name__ == "__main__":
    sys.exit(main())
