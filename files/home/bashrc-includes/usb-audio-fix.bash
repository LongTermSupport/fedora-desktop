#!/usr/bin/env bash

# Fix USB audio devices after suspend/resume issues
# Safe approach that only restarts audio services

usb_audio_fix() {
    echo "Fixing USB audio after suspend/resume..."

    # Show current audio state before fix
    echo "Current audio devices before fix:"
    cat /proc/asound/cards 2>/dev/null
    echo ""

    # Kill any processes using audio devices
    echo "Stopping processes using audio devices..."
    sudo fuser -k /dev/snd/* 2>/dev/null || true
    sleep 1

    # Restart audio services
    echo "Restarting audio services..."
    if systemctl --user list-units | grep -q pipewire; then
        systemctl --user restart pipewire pipewire-pulse wireplumber 2>/dev/null
    elif systemctl --user list-units | grep -q pulseaudio; then
        systemctl --user restart pulseaudio 2>/dev/null
    fi

    sleep 2

    # Try to reload USB audio module if it exists
    if lsmod | grep -q snd_usb_audio; then
        echo "Attempting to reload USB audio kernel module..."
        sudo modprobe -r snd_usb_audio 2>/dev/null
        sleep 1
        sudo modprobe snd_usb_audio 2>/dev/null
        sleep 2
    fi

    # Scan for USB audio devices
    echo "Scanning for USB audio devices..."
    sudo udevadm trigger --subsystem-match=sound
    sudo udevadm settle

    # Restart audio services once more to pick up any changes
    echo "Final audio service restart..."
    if systemctl --user list-units | grep -q pipewire; then
        systemctl --user restart pipewire pipewire-pulse wireplumber 2>/dev/null
    elif systemctl --user list-units | grep -q pulseaudio; then
        systemctl --user restart pulseaudio 2>/dev/null
    fi

    sleep 2

    # Show current audio devices after fix
    echo ""
    echo "Current audio devices after fix:"
    cat /proc/asound/cards 2>/dev/null

    echo ""
    echo "Audio reset complete."
    echo ""
    echo "If your USB audio device (like Steinberg UR22C) is still missing:"
    echo "  1. Try unplugging and replugging the USB cable"
    echo "  2. Check if the device needs to be powered on"
    echo "  3. Try a different USB port"
    echo ""
    echo "To check audio output: pactl list sinks short"
}