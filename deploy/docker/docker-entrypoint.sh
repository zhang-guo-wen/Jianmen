#!/bin/sh
set -eu

data_dir=/app/data
cert_dir=/app/certs

mkdir -p \
  "$data_dir/rdp-spool" \
  "$data_dir/rdp-drive" \
  "$cert_dir"

if [ ! -s "$cert_dir/database.key" ] || \
   [ ! -s "$cert_dir/database.crt" ] || \
   [ ! -s "$cert_dir/database-ca.crt" ] || \
   [ ! -s "$cert_dir/admin.key" ] || \
   [ ! -s "$cert_dir/admin.crt" ]; then
  rm -f \
    "$cert_dir/admin.key" \
    "$cert_dir/admin.crt" \
    "$cert_dir/database.key" \
    "$cert_dir/database.crt" \
    "$cert_dir/database-ca.crt" \
    "$cert_dir/database-ca.srl"

  openssl req -x509 -newkey rsa:3072 -nodes -days 3650 \
    -keyout "$cert_dir/admin.key" \
    -out "$cert_dir/admin.crt" \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1

  openssl req -x509 -newkey rsa:3072 -nodes -days 3650 \
    -keyout /tmp/database-ca.key \
    -out "$cert_dir/database-ca.crt" \
    -subj "/CN=Jianmen local database CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" >/dev/null 2>&1

  openssl req -new -newkey rsa:3072 -nodes \
    -keyout "$cert_dir/database.key" \
    -out /tmp/database.csr \
    -subj "/CN=localhost" >/dev/null 2>&1

  printf '%s\n' \
    'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature,keyEncipherment' \
    'extendedKeyUsage=serverAuth' \
    'subjectAltName=DNS:localhost,IP:127.0.0.1' >/tmp/database.ext

  openssl x509 -req \
    -in /tmp/database.csr \
    -CA "$cert_dir/database-ca.crt" \
    -CAkey /tmp/database-ca.key \
    -CAcreateserial \
    -out "$cert_dir/database.crt" \
    -days 3650 \
    -sha256 \
    -extfile /tmp/database.ext >/dev/null 2>&1

  rm -f \
    "$cert_dir/database-ca.srl" \
    /tmp/database-ca.key \
    /tmp/database.csr \
    /tmp/database.ext
fi

chown -R jianmen:jianmen "$data_dir" "$cert_dir"
chmod 600 "$cert_dir/admin.key" "$cert_dir/database.key"
chmod 644 \
  "$cert_dir/admin.crt" \
  "$cert_dir/database.crt" \
  "$cert_dir/database-ca.crt"

exec su-exec jianmen "$@"
