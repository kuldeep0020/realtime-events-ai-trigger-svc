# Stage 1: build the binary
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Download dependencies before copying source to leverage Docker layer cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/realtime-trigger \
    ./cmd/realtime-trigger

# Stage 2: minimal runtime image
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/realtime-trigger /realtime-trigger

LABEL org.opencontainers.image.title="realtime-events-ai-trigger-svc"

USER nonroot:nonroot

ENTRYPOINT ["/realtime-trigger", "serve"]
