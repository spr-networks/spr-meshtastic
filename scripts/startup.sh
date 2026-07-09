#!/bin/bash
set -a
. /configs/base/config.sh
# optional plugin env (written by install.sh)
[ -f /configs/spr-meshtastic/config.sh ] && . /configs/spr-meshtastic/config.sh
set +a

# No long-running daemon: the plugin shells out to the meshtastic CLI
# (installed in /opt/meshtastic) per request.
exec /meshtastic_plugin
