#!/bin/bash

echo "=== PipeWire Debug Information ==="
echo ""
echo "## System Information"
uname -a
cat /etc/fedora-release
echo ""
echo "## PipeWire Version"
pw-cli --version 2>&1
wireplumber --version 2>&1
echo ""
echo "## PipeWire Status"
systemctl --user status pipewire --no-pager | head -10
echo ""
echo "## Current Configuration"
ls -la ~/.config/pipewire/pipewire.conf.d/
echo ""
echo "## HD Audio Config (99-hd-audio.conf)"
cat ~/.config/pipewire/pipewire.conf.d/99-hd-audio.conf
echo ""
echo "## ALSA Properties Config (99-alsa-properties.conf)"
cat ~/.config/pipewire/pipewire.conf.d/99-alsa-properties.conf
echo ""
echo "## Audio Devices"
pw-cli list-objects | grep -E "node\.name.*alsa" | head -10
echo ""
echo "## Current Sample Rates (pw-top snapshot)"
pw-top -b | head -30
echo ""
echo "## qobuz-player Process Info"
if pgrep -f qobuz-player > /dev/null; then
    echo "qobuz-player PID: $(pgrep -f qobuz-player)"
else
    echo "qobuz-player not running"
fi
echo ""
echo "## PipeWire Graph Info for qobuz-player"
pw-dump | jq '.[] | select(.info.props."application.name" == "PipeWire ALSA [qobuz-player]") | {name: .info.props."application.name", api: .info.props."client.api", node: .info.props."node.name"}' 2>/dev/null || echo "qobuz-player not found in PipeWire graph"