# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy source code first (needed for go mod tidy)
COPY . .

# Download dependencies and generate go.sum
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o apicli .

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
