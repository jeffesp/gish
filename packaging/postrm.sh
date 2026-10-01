#!/bin/sh
# Unregister gish from /etc/shells, but only on real removal, not upgrade.
#   deb: "remove" / "purge" on removal, "upgrade" on upgrade
#   rpm: $1 is the number of remaining installs (0 = removal)
set -e

case "$1" in
    remove|purge|0) ;;
    *) exit 0 ;;
esac

shell=/usr/bin/gish
if [ -f /etc/shells ]; then
    tmp="$(mktemp)"
    grep -vxF "$shell" /etc/shells > "$tmp" || true
    cat "$tmp" > /etc/shells
    rm -f "$tmp"
fi
exit 0
