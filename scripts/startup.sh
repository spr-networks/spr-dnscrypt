#!/bin/bash
set -a
. /configs/base/config.sh
if [ -f /configs/spr-dnscrypt/config.sh ]; then
    . /configs/spr-dnscrypt/config.sh
fi
set +a

STATE_DIR=/state/plugins/spr-dnscrypt
mkdir -p "$STATE_DIR"

# Seed the vendored (build-time pinned, signature-carrying) resolvers list as
# dnscrypt-proxy's source cache so the daemon starts fully offline. dnscrypt-proxy
# refreshes the cache from the network later when it can.
for f in public-resolvers.md public-resolvers.md.minisig; do
    if [ ! -f "$STATE_DIR/$f" ]; then
        cp "/usr/share/dnscrypt-proxy/$f" "$STATE_DIR/$f"
    fi
done

# The plugin binary supervises dnscrypt-proxy as a child process.
exec /dnscrypt_plugin
