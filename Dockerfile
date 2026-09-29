# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Download dependencies first so this layer is cached until go.mod/go.sum change
COPY go.mod go.sum ./
RUN go mod download

# Build the binary from the committed, locked dependency set
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-X api/cmd.version=${VERSION}" -o apicli .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN adduser -D -u 1000 -h /home/appuser appuser

# Switch to non-root user
USER appuser
WORKDIR /home/appuser

# Copy the binary from builder
COPY --from=builder /app/apicli .

# Create data directory with correct ownership (already owned by appuser due to USER directive)
RUN mkdir -p /home/appuser/.apicli

ENTRYPOINT ["./apicli"]
