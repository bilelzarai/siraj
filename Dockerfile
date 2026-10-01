# ---- build -------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# templ output is committed, so the image does not need the templ CLI.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
# The admin CLI ships too: the first admin cannot be created through the web UI,
# so without it a fresh deployment has no way to grant anyone privilege.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sirajctl ./cmd/sirajctl

# ---- run ---------------------------------------------------------------
FROM alpine:3.20

RUN adduser -D -u 10001 app && apk add --no-cache ca-certificates tzdata
USER app

WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/sirajctl /app/sirajctl

EXPOSE 8080

# The binary is its own probe, so the image needs no curl and no shell for this.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/server", "-healthcheck"]

ENTRYPOINT ["/app/server"]
