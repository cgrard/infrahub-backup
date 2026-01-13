# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binaries
RUN make build-all

# Runtime stage
FROM alpine:3.23

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    kubectl \
    && update-ca-certificates

# Create non-root user
RUN addgroup -g 1000 infrahub && \
    adduser -D -u 1000 -G infrahub infrahub

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/bin/infrahub-backup /usr/local/bin/
COPY --from=builder /build/bin/infrahub-taskmanager /usr/local/bin/

# Set permissions
RUN chmod +x /usr/local/bin/infrahub-backup /usr/local/bin/infrahub-taskmanager

# Switch to non-root user
USER infrahub

# Default command
ENTRYPOINT ["/usr/local/bin/infrahub-backup"]
CMD ["--help"]
