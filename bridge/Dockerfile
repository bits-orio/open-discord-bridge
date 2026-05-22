# ── Build ────────────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /out/odb-bridge ./cmd/bridge

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 bridge
COPY --from=build /out/odb-bridge /usr/local/bin/odb-bridge
# Ship the OpenAPI spec for reference inside the image.
COPY --from=build /src/pkg/controlapi/spec/openapi.yaml /etc/open-discord-bridge/openapi.yaml
USER bridge
EXPOSE 7777
ENTRYPOINT ["/usr/local/bin/odb-bridge"]
CMD ["-config", "/etc/open-discord-bridge/bridge.yaml"]
