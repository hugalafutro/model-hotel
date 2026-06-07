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

# Grant appuser access to the Docker socket (if mounted).
# The socket is typically owned by root:docker (GID varies by host) with mode
# 0660, so the non-root appuser cannot connect.  su-exec sets only the primary
# UID/GID (no supplementary groups), so we must make the socket's group the
# process's primary GID.  We create an in-container group matching the
# socket's GID and pass it to su-exec.
DOCKER_SOCKET_GROUP=""
if [ -S /var/run/docker.sock ]; then
	socket_gid=$(stat -c '%g' /var/run/docker.sock 2>/dev/null || true)
	# GID 0 means the socket is root-owned; we intentionally skip that case
	# because no safe group membership can grant access without root privileges.
	if [ -n "$socket_gid" ] && [ "$socket_gid" != "0" ]; then
		if getent group "$socket_gid" >/dev/null 2>&1; then
			DOCKER_SOCKET_GROUP=$(getent group "$socket_gid" | cut -d: -f1)
		elif addgroup -g "$socket_gid" -S "docker-socket" 2>/dev/null; then
			DOCKER_SOCKET_GROUP="docker-socket"
		fi
	fi
fi

# Drop privileges and run the server.
# When the Docker socket is mounted, use its group as the primary GID so the
# process can connect.  Otherwise, use appuser's own group.
if [ -n "$DOCKER_SOCKET_GROUP" ]; then
	exec su-exec "appuser:$DOCKER_SOCKET_GROUP" "$@"
else
	exec su-exec appuser:appuser "$@"
fi
