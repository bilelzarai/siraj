# Production image: the front-end is built with Bun (Vite 8 / Rolldown + Tailwind v4),
# then embedded in the Go binary (web/embed.go), which ships alone on distroless.

# 1 · assets ------------------------------------------------------------------
FROM oven/bun:1 AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY vite.config.ts tsconfig.json ./
COPY web/ ./web/
# Two entries: the player app (assets/js/main.ts) and the admin console (admin.ts).
RUN bun run build

# 2 · server ------------------------------------------------------------------
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The Vite output is git-ignored, so it comes from the assets stage.
COPY --from=assets /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# 3 · runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/ /app/
ENV APP_ENV=production HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/server"]
