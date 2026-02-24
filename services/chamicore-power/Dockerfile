# Multi-stage Dockerfile for chamicore-power

FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildDate=${BUILD_DATE}" \
    -o /bin/power \
    ./cmd/chamicore-power

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="chamicore-power" \
      org.opencontainers.image.description="OpenCHAMI power service" \
      org.opencontainers.image.source="https://git.cscs.ch/openchami/chamicore-power" \
      org.opencontainers.image.vendor="OpenCHAMI" \
      org.opencontainers.image.licenses="MIT"

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/power /bin/power
COPY migrations /migrations

USER 65534:65534

EXPOSE 27775

ENTRYPOINT ["/bin/power"]
