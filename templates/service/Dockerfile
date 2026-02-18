# TEMPLATE: Multi-stage Dockerfile for __SERVICE_FULL__
# Copy this file and replace all __PLACEHOLDER__ markers with your service values.

# ===========================================================================
# Stage 1: Build
# ===========================================================================
FROM golang:1.23-alpine AS builder

# Build arguments injected at docker build time.
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Install certificates and timezone data (needed at runtime for TLS and time).
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildDate=${BUILD_DATE}" \
    -o /bin/__SERVICE__ \
    ./cmd/service

# ===========================================================================
# Stage 2: Runtime (distroless)
# ===========================================================================
FROM gcr.io/distroless/static-debian12:nonroot

# Labels following OCI image spec.
LABEL org.opencontainers.image.title="__SERVICE_FULL__" \
      org.opencontainers.image.description="OpenCHAMI __SERVICE__ service" \
      org.opencontainers.image.source="https://git.cscs.ch/openchami/__SERVICE_FULL__" \
      org.opencontainers.image.vendor="OpenCHAMI" \
      org.opencontainers.image.licenses="MIT"

# Copy artifacts from builder.
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/__SERVICE__ /bin/__SERVICE__

# Copy migrations so they are available at runtime.
COPY migrations /migrations

# Run as non-root user (65534 = nobody in distroless).
USER 65534:65534

# TEMPLATE: Expose the default port for this service.
EXPOSE __PORT__

ENTRYPOINT ["/bin/__SERVICE__"]
