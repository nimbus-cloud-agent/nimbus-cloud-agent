#!/bin/sh
# Render injects $PORT at runtime. Nimbus reads its
# listen address from NIMBUS_ADDR / --addr, so map $PORT here instead of
# hard-coding a port in the Dockerfile. An explicit NIMBUS_ADDR wins.
set -eu
if [ -z "${NIMBUS_ADDR:-}" ]; then
  NIMBUS_ADDR="0.0.0.0:${PORT:-8080}"
  export NIMBUS_ADDR
fi
# Turn working files are ephemeral in a container; PostgreSQL is the source
# of truth, so default them to /tmp unless the operator sets a volume path.
if [ -z "${NIMBUS_WORK_DIR:-}" ]; then
  NIMBUS_WORK_DIR="/tmp/turns"
  export NIMBUS_WORK_DIR
fi
exec nimbus-cloud-agent "$@"
