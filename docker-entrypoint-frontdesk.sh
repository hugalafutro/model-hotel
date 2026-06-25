#!/bin/sh
set -e

# Front Desk writes its embedded SQLite database and the admin-token hash file
# under DATA_DIR. Fix ownership so the non-root appuser can write to a freshly
# created bind mount, then drop privileges. Unlike the main image there is no
# Postgres data directory to avoid here, so a plain recursive chown is safe.
DATA_DIR="${DATA_DIR:-/data}"
mkdir -p "$DATA_DIR"
chown -R appuser:appuser "$DATA_DIR"

exec su-exec appuser:appuser "$@"
