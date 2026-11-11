# ---- assets ------------------------------------------------------------
# This stage runs BEFORE the compile stage, not beside it. The asset tree is
# embedded into the binary at compile time, so a parallel stage produces an
# image with an empty asset directory and a page that links nothing.
FROM node:22-alpine AS assets

WORKDIR /src

# The lockfile first, so a source-only change reuses the install layer. `npm ci`
# rather than `npm install`: the lockfile is the input, exactly.
COPY package.json package-lock.json ./
RUN npm ci

COPY vite.config.js ./
COPY web ./web
RUN npm run build

# ---- build -------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The built assets, in before the compiler runs. The build context excludes
# static/dist/, so this copy is the only one and a stale local build cannot
# shadow it.
COPY --from=assets /src/static/dist ./static/dist

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

# Where photos and voice notes go, created here rather than at the first
# upload. A named volume mounted over a path that does not exist in the image
# is created root-owned, and this process is uid 10001 — the upload fails with
# a permission error at the moment somebody sends their first attachment.
# Created by the app user (USER is already set), the volume inherits that owner.
RUN mkdir -p /app/data/uploads

EXPOSE 8080

# The binary is its own probe, so the image needs no curl and no shell for this.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/server", "-healthcheck"]

ENTRYPOINT ["/app/server"]
