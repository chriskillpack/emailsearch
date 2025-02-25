# syntax=docker.io/docker/dockerfile:1.7-labs
# ^--- for COPY --exclude syntax
ARG GO_VERSION=1
FROM golang:${GO_VERSION}-bookworm as builder

# Stage 1: Build the Go binary
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
# Copy everything but email_index
COPY --exclude=email_index . .
RUN go build -v -o /run-app ./cmd/search


# Stage 2: Create minimal runtime with data
FROM debian:bookworm AS data
RUN mkdir -p /data
COPY ./email_index /data

# Stage 3: Final image
FROM debian:bookworm

# Copy the built binary in
COPY --from=builder /run-app /usr/local/bin/
# Copy the data in
COPY --from=data /data /data

ENTRYPOINT ["run-app", "--indexdir=/data"]
