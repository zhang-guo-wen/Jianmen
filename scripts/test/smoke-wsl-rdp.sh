#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <repository-root>" >&2
  exit 2
fi

root=$1
binary="$root/dist/jianmen-linux-amd64-rdp"
config="$root/configs/config.wsl.rdp.example.json"

[[ -f "$binary" ]] || {
  echo "Linux RDP binary is missing: $binary" >&2
  exit 1
}
[[ -f "$config" ]] || {
  echo "WSL RDP config is missing: $config" >&2
  exit 1
}

test_dir=$(mktemp -d /tmp/jianmen-rdp-smoke.XXXXXX)
jianmen_pid=""

cleanup() {
  if [[ -n "$jianmen_pid" ]] && kill -0 "$jianmen_pid" 2>/dev/null; then
    kill "$jianmen_pid" 2>/dev/null || true
    for _ in $(seq 1 20); do
      kill -0 "$jianmen_pid" 2>/dev/null || break
      sleep 0.1
    done
    kill -KILL "$jianmen_pid" 2>/dev/null || true
    wait "$jianmen_pid" 2>/dev/null || true
  fi
  case "$test_dir" in
    /tmp/jianmen-rdp-smoke.*) rm -rf -- "$test_dir" ;;
    *) echo "refusing to remove unexpected test directory: $test_dir" >&2 ;;
  esac
}
trap cleanup EXIT

cp "$binary" "$test_dir/jianmen"
cp "$config" "$test_dir/config.json"
chmod 0755 "$test_dir/jianmen"

cd "$test_dir"
./jianmen -config ./config.json >jianmen.log 2>&1 &
jianmen_pid=$!

healthy=false
for _ in $(seq 1 120); do
  if ! kill -0 "$jianmen_pid" 2>/dev/null; then
    echo "Linux RDP process exited before becoming healthy" >&2
    tail -n 120 jianmen.log >&2
    exit 1
  fi
  if command -v curl >/dev/null 2>&1; then
    if curl --fail --silent --max-time 1 http://127.0.0.1:47100/api/init/status >/dev/null; then
      healthy=true
      break
    fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q -T 1 -O /dev/null http://127.0.0.1:47100/api/init/status; then
      healthy=true
      break
    fi
  else
    echo "curl or wget is required for the WSL RDP smoke test" >&2
    exit 1
  fi
  sleep 0.5
done

if [[ "$healthy" != true ]]; then
  echo "Linux RDP process did not become healthy" >&2
  tail -n 120 jianmen.log >&2
  exit 1
fi

guacd_process=$(pgrep -a -P "$jianmen_pid" || true)
if [[ -z "$guacd_process" ]] || ! grep -q "guacd" <<<"$guacd_process"; then
  echo "embedded guacd child process was not found" >&2
  tail -n 120 jianmen.log >&2
  exit 1
fi

if ! grep -q "managed guacd is ready" jianmen.log; then
  echo "managed guacd readiness was not logged" >&2
  tail -n 120 jianmen.log >&2
  exit 1
fi

echo "WSL_RDP_HEALTH=ok"
echo "JIANMEN_PID=$jianmen_pid"
echo "GUACD_PROCESS=$guacd_process"
if command -v ss >/dev/null 2>&1; then
  ss -lntp | grep -E ':(47100|47102|33060|4822)\b' || true
fi
tail -n 30 jianmen.log
