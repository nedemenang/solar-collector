#!/usr/bin/env bash
# Creates the Toxiproxy proxy that sits in front of Postgres. Run this once
# after `docker compose up`, then use toggle-partition.sh to cut/restore
# the link on demand.
set -euo pipefail

TOXIPROXY_API="${TOXIPROXY_API:-http://localhost:8474}"

curl -sf -X POST "${TOXIPROXY_API}/proxies" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "postgres",
    "listen": "0.0.0.0:5433",
    "upstream": "postgres:5432",
    "enabled": true
  }' && echo "toxiproxy: postgres proxy created on :5433"
