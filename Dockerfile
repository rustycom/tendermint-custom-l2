# Build stage: compile kvstore-app and tendermint
FROM golang:1.21-bookworm AS builder
WORKDIR /build

# Tendermint version used by the app
ARG TENDERMINT_VERSION=v0.34.24

# Copy go mod and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build kvstore-app
COPY . .
RUN CGO_ENABLED=0 go build -o kvstore-app .

# Install tendermint binary (used by entrypoint and setup_validators)
RUN go install github.com/tendermint/tendermint/cmd/tendermint@${TENDERMINT_VERSION}

# Runtime stage: run 3 nodes in one container
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy built binary and tendermint from builder
COPY --from=builder /build/kvstore-app /app/kvstore-app
COPY --from=builder /go/bin/tendermint /usr/local/bin/tendermint

# Copy scripts (init, single-node entrypoint, and setup_validators)
COPY docker/entrypoint.sh /app/entrypoint.sh
COPY docker/entrypoint-init.sh /app/entrypoint-init.sh
COPY docker/entrypoint-node.sh /app/entrypoint-node.sh
COPY scripts/setup_validators.sh /app/scripts/setup_validators.sh
RUN chmod +x /app/entrypoint.sh /app/entrypoint-init.sh /app/entrypoint-node.sh /app/scripts/setup_validators.sh

# setup_validators.sh expects to run from repo root and uses NODES_DIR, ROOT_DIR
# It also runs tendermint init and python; we need python3 in path as python3
# Tendermint is in PATH. When running from scratch, setup will create nodes/node1,2,3
# and we'll mount a volume at /app/nodes and /app/data1,2,3 so data persists.
# If user removes volumes and starts again, entrypoint sees no state → runs setup → height 0.

ENV APP_ROOT=/app
EXPOSE 26656 26657 26660 26661 26663 26664

# Default: run single node (NODE_INDEX=1,2,3). Init container overrides with entrypoint-init.sh.
ENTRYPOINT ["/app/entrypoint-node.sh"]
