FROM cgr.dev/chainguard/go:latest-dev AS builder

LABEL org.opencontainers.image.source="https://github.com/nice-pink/itakeit-messenger"

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 ./build

# The claude-code backend runs the Claude Code CLI, so the runtime image carries it.
FROM node:22-slim AS runner

LABEL org.opencontainers.image.source="https://github.com/nice-pink/itakeit-messenger"

# Same CLI version itakeit-agent is verified against; the flags and JSON fields the
# classifier reads are the ones it uses.
ARG CLAUDE_CODE_VERSION=2.1.285
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} && npm cache clean --force
COPY --from=builder /app/bin/itakeit-messenger /app/itakeit-messenger
USER node
ENTRYPOINT [ "/app/itakeit-messenger", "-config", "/config/config.yaml" ]
