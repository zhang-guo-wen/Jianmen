# Container and release publishing

## Container images

Normal branch pushes and pull requests run CI checks only. They never publish a container image or
GitHub release. A semantic version tag such as `v1.2.3` or `v1.2.3-rc.1` publishes the
multi-architecture container image and release archives. The image supports `linux/amd64` and
`linux/arm64`. Only a stable tag updates `latest`; a prerelease tag does not.

The image fails closed unless `/app/certs/admin.crt` and
`/app/certs/admin.key` are mounted. Create a short-lived self-signed
certificate volume for local evaluation:

```bash
docker volume create jianmen-certs
docker run --rm --user 0 \
  -v jianmen-certs:/certs \
  alpine:3.23 sh -c \
  'apk add --no-cache openssl &&
   openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
     -keyout /certs/admin.key -out /certs/admin.crt \
     -subj "/CN=localhost" \
     -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" &&
   openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
     -keyout /tmp/database-ca.key -out /certs/database-ca.crt \
     -subj "/CN=Jianmen local database CA" \
     -addext "basicConstraints=critical,CA:TRUE" \
     -addext "keyUsage=critical,keyCertSign,cRLSign" &&
   openssl req -new -newkey rsa:3072 -nodes \
     -keyout /certs/database.key -out /tmp/database.csr \
     -subj "/CN=localhost" &&
   printf "%s\n" \
     "basicConstraints=critical,CA:FALSE" \
     "keyUsage=critical,digitalSignature,keyEncipherment" \
     "extendedKeyUsage=serverAuth" \
     "subjectAltName=DNS:localhost,IP:127.0.0.1" >/tmp/database.ext &&
   openssl x509 -req -in /tmp/database.csr \
     -CA /certs/database-ca.crt -CAkey /tmp/database-ca.key -CAcreateserial \
     -out /certs/database.crt -days 30 -sha256 -extfile /tmp/database.ext &&
   rm -f /certs/database-ca.srl /tmp/database-ca.key /tmp/database.csr /tmp/database.ext &&
   chown 10001:10001 /certs/admin.key /certs/admin.crt /certs/database.key /certs/database.crt /certs/database-ca.crt &&
   chmod 600 /certs/admin.key /certs/database.key &&
   chmod 644 /certs/admin.crt /certs/database.crt /certs/database-ca.crt'
```

Run the latest released image with the Admin HTTPS port bound only to the host
loopback interface:

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  -v jianmen-certs:/app/certs:ro \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

The default container endpoints are:

- Web administration (local evaluation only): `https://127.0.0.1:47100`
- SSH gateway: `HOST:47102`
- Unified database gateway (default, MySQL/PostgreSQL/Redis): `HOST:33060`
- Independent MySQL gateway: `HOST:33061`
- Independent PostgreSQL gateway: `HOST:33062`
- Independent Redis gateway: `HOST:33063`
- Application gateways: `HOST:47110-47199`

Mount a custom configuration file at `/app/config.json` when the defaults in
`config.docker.json` are not suitable. The default image never enables
plaintext Admin HTTP. For a reverse-proxy deployment, use
`config.docker.proxy.example.json`, keep the Jianmen container and proxy on an
isolated Docker network, and do not publish Jianmen's port `47100`. The complete
Caddy command sequence and the Nginx Stream database-gateway precautions are
documented in `README.md`.

The default `unified` mode lets native MySQL, PostgreSQL, and Redis clients share port `33060`.
MySQL connections wait for the 200 ms protocol-detection window; established-session throughput
is unaffected. The alternative `independent` mode uses ports `33061`, `33062`, and `33063`.
Only the selected mode binds its listeners. The default container command publishes only `33060`;
when selecting `independent`, also publish `33061:33061`, `33062:33062`, and `33063:33063`
(or add those mappings to Compose) before restarting the container.

Client-facing TLS has two policies: `optional` (the default) accepts both plaintext and TLS for
MySQL, PostgreSQL, and Redis, while `required` rejects plaintext authentication and database
traffic. PostgreSQL plaintext `CancelRequest` control packets remain compatible because they
carry no login credentials or database data and must match the per-session cancellation secret.
The default Docker configuration uses `database.crt`, `database.key`, and `database-ca.crt` as a
local custom-CA example. For a publicly trusted certificate, configure a leaf-first full-chain
`cert_file`, its matching `key_file`, and a `server_name` covered by the certificate SAN, and omit
`ca_file`. Jianmen validates that chain against the runtime system certificate pool during startup
and fails closed if the chain, validity period, key usage, or hostname is invalid. The certificate
file must contain every required intermediate certificate.

The gateway API reports whether the validated identity uses `custom` or `system` trust and never
exposes private-key material. DBeaver uses its Java default trust store for system-trusted
certificates. `psql` system trust requires libpq 16 or newer; older clients and the native MySQL CLI
still require an explicit CA file. No client path silently falls back to encryption without
identity verification.

## GitHub releases

Create and push a semantic version tag to build and publish release archives:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow builds these archives with the Vue frontend embedded in the Go binary:

- Windows amd64
- Windows arm64
- Linux amd64 Lite (no embedded guacd runtime)
- Linux amd64 RDP (self-extracting embedded guacd runtime)
- Linux arm64 Lite (no embedded guacd runtime)
- Linux arm64 RDP (self-extracting embedded guacd runtime)

Each archive includes the executable, `config.example.json`, `README.md`, and `LICENSE`. RDP
archives also include `THIRD_PARTY_NOTICES.md`. The release contains `checksums.txt` with SHA-256
checksums. RDP archives are built from the pinned official guacd image; the target host does not
need Docker or a preinstalled guacd.
