#!/bin/sh
set -eu

data_dir=/app/data

mkdir -p \
  "$data_dir/rdp-spool" \
  "$data_dir/rdp-drive"

chown -R jianmen:jianmen "$data_dir"

exec su-exec jianmen "$@"
