# Container and release publishing

## Container images

Every push to `dev` publishes a multi-architecture image to GitHub Container Registry:

```text
ghcr.io/zhang-guo-wen/jianmen:dev
```

Every `v*` tag publishes version, major/minor, `latest`, and commit SHA tags. The image supports `linux/amd64` and `linux/arm64`.

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

Run the development image with the Admin HTTPS port bound only to the host
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
  ghcr.io/zhang-guo-wen/jianmen:dev
```

The default container endpoints are:

- Web administration (local evaluation only): `https://127.0.0.1:47100`
- SSH gateway: `HOST:47102`
- Unified database gateway (default, MySQL/PostgreSQL/Redis): `HOST:33060`
- Independent MySQL gateway: `HOST:33061`
- Independent PostgreSQL gateway (TLS required): `HOST:33062`
- Independent Redis gateway (remote AUTH requires TLS): `HOST:33063`
- Application gateways: `HOST:47110-47199`

Mount a custom configuration file at `/app/config.json` when the defaults in
`config.docker.json` are not suitable. The default image never enables
plaintext Admin HTTP. For a reverse-proxy deployment, use
`config.docker.proxy.example.json`, keep the Jianmen container and proxy on an
isolated Docker network, and do not publish Jianmen's port `47100`. The complete
Caddy command sequence is documented in `README.md`.

The default `unified` mode lets native MySQL, PostgreSQL, and Redis clients share port `33060`.
MySQL connections wait for the 200 ms protocol-detection window; established-session throughput
is unaffected. The alternative `independent` mode uses ports `33061`, `33062`, and `33063`.
Only the selected mode binds its listeners. The default container command publishes only `33060`;
when selecting `independent`, also publish `33061:33061`, `33062:33062`, and `33063:33063`
(or add those mappings to Compose) before restarting the container.

PostgreSQL must negotiate TLS before its cleartext password exchange, while Redis only permits
plaintext AUTH from a loopback client for local development. The default Docker configuration
therefore requires `/app/certs/database.crt` and `/app/certs/database.key`. For client identity
verification, also mount the public CA at
`/app/certs/database-ca.crt` and configure each listener's `ca_file` plus a `server_name` that
matches a certificate SAN. The gateway API distributes only validated certificate PEM, the TLS
server name, and the leaf-certificate SHA-256 fingerprint; it never exposes `key_file` or private
key contents. Without complete TLS identity material, the web UI does not offer a downgraded
database connection command. The local certificate command above creates `database-ca.crt` and a
separate ServerAuth leaf certificate whose `localhost` SAN matches the Docker examples'
`server_name`. A CA-issued leaf without `ca_file` is rejected; the fallback is reserved for a
single valid self-signed leaf used with explicit certificate-pin semantics.

## GitHub releases

Create and push a version tag to publish release archives:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow builds these archives with the Vue frontend embedded in the Go binary:

- Windows amd64
- Windows arm64
- Linux amd64
- Linux arm64

Each archive includes the executable, `config.example.json`, `README.md`, and `LICENSE`. The release also contains `checksums.txt` with SHA-256 checksums.
