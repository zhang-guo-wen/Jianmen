ARG GUACD_IMAGE=guacamole/guacd:1.6.0@sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f

FROM ${GUACD_IMAGE}

# Run .\build.ps1 on Windows before building this Linux/amd64 runtime image.
LABEL org.opencontainers.image.source="https://github.com/zhang-guo-wen/Jianmen"
LABEL org.opencontainers.image.description="Jianmen bastion host with managed guacd 1.6.0"
LABEL org.opencontainers.image.licenses="MIT"

USER root

RUN addgroup -S -g 10001 jianmen \
    && adduser -S -D -H -u 10001 -G jianmen jianmen \
    && mkdir -p /app/data /app/data/rdp-spool /app/data/rdp-drive \
    && chown -R jianmen:jianmen /app

WORKDIR /app

COPY --chown=jianmen:jianmen --chmod=0555 dist/bastion-core-linux-amd64 /app/jianmen
COPY --chown=jianmen:jianmen --chmod=0444 config.docker.web-rdp.example.json /app/config.json

USER jianmen

VOLUME ["/app/data"]

EXPOSE 47100 47102 33060 33061 33062 33063 47110-47199

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --no-check-certificate -O /dev/null https://127.0.0.1:47100/api/init/status \
        || wget -q -O /dev/null http://127.0.0.1:47100/api/init/status \
        || exit 1

STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/jianmen"]
CMD ["-config", "/app/config.json"]
