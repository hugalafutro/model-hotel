#!/bin/sh
set -e

# Fix ownership of app-writable paths under /data.
# Do NOT recurse into /data/pgdata — those files belong to the PostgreSQL
# container and changing ownership would break it.
mkdir -p /data/backups
chown appuser:appuser /data
chown appuser:appuser /data/backups

# Fix admin-token ownership if it exists (upgrade path from root-run containers)
if [ -f /data/admin-token ]; then
	chown appuser:appuser /data/admin-token
fi

# Drop privileges and run the server
exec su-exec appuser:appuser "$@"
