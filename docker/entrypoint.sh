#!/usr/bin/env bash
set -euo pipefail

# Backwards-compatible wrapper. The multi-container Docker Compose setup uses:
# - init container: /app/entrypoint-init.sh
# - node containers: /app/entrypoint-node.sh (default ENTRYPOINT)
#
# If someone still runs an image that calls /app/entrypoint.sh, we run a single node.
exec /app/entrypoint-node.sh "$@"

