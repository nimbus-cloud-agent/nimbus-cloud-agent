# syntax=docker/dockerfile:1
# Nimbus dev image: Go control plane (frontend embedded) + the separately
# packaged nimbus-agent-runtime, both built from this repository. Nothing is
# pulled from npm at deploy time except the pinned dependencies locked in
# package-lock.json / web/package-lock.json.
#
# Build:  docker build -t nimbus-cloud-agent:dev .
# Run:    docker run -e NIMBUS_POSTGRES=... -e NIMBUS_E2B_API_KEY=... \
#           -e NIMBUS_PUBLIC_HOST=... -e NIMBUS_BASIC_USER=... \
#           -e NIMBUS_BASIC_PASS=... -p 8080:8080 nimbus-cloud-agent:dev
# Render sets $PORT at runtime; docker-entrypoint.sh maps it to NIMBUS_ADDR.

FROM golang:1.25-bookworm AS builder

# Node 24.11+ is required by nimbus-agent-runtime (engines >= 24.11.0).
ARG NODE_VERSION=24.11.0
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      -o /tmp/node.tar.xz \
    && apt-get update \
    && apt-get install -y --no-install-recommends xz-utils \
    && tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1 \
    && rm /tmp/node.tar.xz \
    && rm -rf /var/lib/apt/lists/* \
    && node --version && npm --version && go version

WORKDIR /src
COPY Makefile ./
COPY package.json package-lock.json ./
COPY runtime/ runtime/
COPY web/ web/
COPY protocol/ protocol/
COPY scripts/ scripts/
COPY go.mod go.sum ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY vmrelay/ vmrelay/
# The Makefile's `build` target builds the runtime, then the web frontend,
# then the Go binary with the frontend embedded. `pack-runtime` produces
# release/nimbus-agent-runtime-0.3.0.tgz, installed globally below so the
# Go binary finds `nimbus-agent-runtime` on PATH at runtime.
RUN npm ci \
    && make build \
    && make pack-runtime \
    && npm install -g ./release/nimbus-agent-runtime-*.tgz \
    && nimbus-agent-runtime --version 2>/dev/null || true \
    && ./bin/nimbus-cloud-agent version

FROM node:24-bookworm-slim AS runtime

RUN useradd -m -u 10001 nimbus
COPY --from=builder /src/bin/nimbus-cloud-agent /usr/local/bin/nimbus-cloud-agent
COPY --from=builder /usr/local/bin/nimbus-agent-runtime /usr/local/bin/nimbus-agent-runtime
COPY --from=builder /usr/local/lib/node_modules/nimbus-agent-runtime /usr/local/lib/node_modules/nimbus-agent-runtime
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh \
    && mkdir -p /tmp/turns && chown -R nimbus:nimbus /tmp/turns

USER nimbus
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["serve"]
