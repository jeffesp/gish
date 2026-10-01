#!/bin/sh
# Register gish as a login shell. Idempotent; runs on install and upgrade.
set -e

shell=/usr/bin/gish
if [ -f /etc/shells ]; then
    grep -qxF "$shell" /etc/shells || echo "$shell" >> /etc/shells
fi
exit 0
