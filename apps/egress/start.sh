#!/bin/bash
set -e

Xvfb :99 -screen 0 1920x1080x24 -ac &
sleep 2

# Chromium's tab audio has nowhere to go under bare Xvfb (no session/user
# audio server). Give it one: a PulseAudio daemon with a null-sink whose
# monitor FFmpeg can capture from, so recordings carry real participant
# audio instead of ffmpeg's anullsrc silence.
mkdir -p /tmp/pulse
export XDG_RUNTIME_DIR=/tmp/pulse
pulseaudio -D --exit-idle-time=-1 --disallow-exit --log-target=stderr
sleep 1
pactl load-module module-null-sink sink_name="${PULSE_SINK:-egress_sink}" sink_properties=device.description=EgressSink
pactl set-default-sink "${PULSE_SINK:-egress_sink}"

node worker.js
