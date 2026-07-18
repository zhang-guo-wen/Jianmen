# Container and release publishing

## Container images

Every push to `dev` publishes a multi-architecture image to GitHub Container Registry:

```text
ghcr.io/zhang-guo-wen/jianmen:dev
```

Every `v*` tag publishes version, major/minor, `latest`, and commit SHA tags. The image supports `linux/amd64` and `linux/arm64`.

Run the development image for local evaluation with the Admin HTTP port bound only to the host loopback interface:

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  ghcr.io/zhang-guo-wen/jianmen:dev
```

The default container endpoints are:

- Web administration (local evaluation only): `http://127.0.0.1:47100`
- SSH gateway: `HOST:47102`
- Database gateway: `HOST:33060`
- Application gateways: `HOST:47110-47199`

Mount a custom configuration file at `/app/config.json` when the defaults in `config.docker.json` are not suitable.
For production, terminate HTTPS at a reverse proxy on the same controlled network or configure
`admin.tls.cert_file` and `admin.tls.key_file`; never publish the container's plaintext Admin port
directly to an untrusted network.

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
