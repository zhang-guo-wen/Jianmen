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
   chown 10001:10001 /certs/admin.key /certs/admin.crt &&
   chmod 600 /certs/admin.key && chmod 644 /certs/admin.crt'
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
- Database gateway: `HOST:33060`
- Application gateways: `HOST:47110-47199`

Mount a custom configuration file at `/app/config.json` when the defaults in
`config.docker.json` are not suitable. The default image never enables
plaintext Admin HTTP. For a reverse-proxy deployment, use
`config.docker.proxy.example.json`, keep the Jianmen container and proxy on an
isolated Docker network, and do not publish Jianmen's port `47100`. The complete
Caddy command sequence is documented in `README.md`.

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
