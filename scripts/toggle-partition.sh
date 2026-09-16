#!/usr/bin/env bash
# Cuts or restores the collector's link to Postgres through Toxiproxy, to
# demonstrate that the collector keeps polling/buffering/alarming locally
# while disconnected, then replays once the link is back.
#
# Usage: ./toggle-partition.sh down   (cuts the link)
#        ./toggle-partition.sh up     (restores it)
set -euo pipefail

TOXIPROXY_API="${TOXIPROXY_API:-http://localhost:8474}"
ACTION="${1:-}"

case "$ACTION" in
  down)
    curl -sf -X POST "${TOXIPROXY_API}/proxies/postgres" \
      -H "Content-Type: application/json" \
      -d '{"enabled": false}' && echo "toxiproxy: postgres link CUT"
    ;;
  up)
    curl -sf -X POST "${TOXIPROXY_API}/proxies/postgres" \
      -H "Content-Type: application/json" \
      -d '{"enabled": true}' && echo "toxiproxy: postgres link RESTORED"
    ;;
  *)
    echo "usage: $0 [down|up]" >&2
    exit 1
    ;;
esac
