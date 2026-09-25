# syntax=docker/dockerfile:1
# check=skip=FromPlatformFlagConstDisallowed

# HIVE - ARM64 (linux/arm64) image.
# Build stage runs on the host's native platform and cross-compiles for ARM64
# (pure Go, pgx needs no cgo), so no QEMU emulation is needed for the compile.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY migrations/ migrations/
COPY templates/ templates/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -trimpath -ldflags="-s -w" -o /out/hive .

# Runtime stage is always ARM64, whatever the build host is.
FROM --platform=linux/arm64 gcr.io/distroless/static-debian12:nonroot

WORKDIR /opt/hive
COPY --from=build /out/hive /opt/hive/hive

ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/opt/hive/hive"]
