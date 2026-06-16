#!/usr/bin/env bash
#
# agent-box image entrypoint: prepare authorized_keys + host key, then run
# dropbear in the foreground with a forced command pinning every session to
# agent-box. See images/agent-box/Dockerfile.
set -euo pipefail

# The daemon mounts the box's authorized_keys Secret at
# /etc/agent-box/authorized_keys; dropbear reads ~/.ssh/authorized_keys, so
# point there. The file may not exist until the Secret is mounted — dropbear
# simply rejects every login until it appears (fail closed).
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
if [ -e /etc/agent-box/authorized_keys ]; then
  ln -sf /etc/agent-box/authorized_keys "$HOME/.ssh/authorized_keys"
fi

# Host key in a writable per-container dir. Regenerated each start; the gateway
# uses ignore_hostkey today (host-key pinning is a follow-up), so a fresh key
# per restart is fine.
KEYDIR="$HOME/.dropbear"
mkdir -p "$KEYDIR"
HOSTKEY="$KEYDIR/ed25519_host_key"
[ -f "$HOSTKEY" ] || dropbearkey -t ed25519 -f "$HOSTKEY" >/dev/null 2>&1

# dropbear flags:
#   -F  foreground (PID 1)            -E  log to stderr
#   -s  disable password auth         -j -k  no local/remote port forwarding
#   -p 2222  unprivileged port        -r  host key file
#   -c  forced command — every session runs agent-box, nothing else
exec dropbear -F -E -s -j -k -p 2222 -r "$HOSTKEY" -c /usr/local/bin/agent-box
