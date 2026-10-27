# Build Stage
FROM golang:1.27.3-alpine AS builder
WORKDIR /app
COPY go.mod ./
# COPY go.sum ./ # Uncomment if using dependencies
RUN go mod download
COPY *.go ./
# -ldflags="-w -s" strips debug info for smaller binary
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o sidecar .

# Runtime Stage
# "static-debian12" includes root CA certs needed for HTTPS (ntfy.sh); nonroot runs as 65532, as the deployment expects
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /app/sidecar /sidecar
CMD ["/sidecar"]
