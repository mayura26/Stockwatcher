#!/bin/sh
set -e
if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  chown 65532:65532 /data
  chmod 0777 /data
  exec su-exec 65532:65532 /usr/local/bin/stockwatcher "$@"
fi
exec /usr/local/bin/stockwatcher "$@"
