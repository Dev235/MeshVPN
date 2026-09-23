# Multi-Stage Build Dockerfile for MeshVPN
FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/meshvpn-server ./cmd/meshvpn-server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/meshvpn-relay ./cmd/meshvpn-relay
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/meshvpnd ./cmd/meshvpnd
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/meshvpn ./cmd/meshvpn

# Control Server Target
FROM alpine:3.18 AS server
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/meshvpn-server /usr/local/bin/meshvpn-server
COPY --from=builder /bin/meshvpn /usr/local/bin/meshvpn
EXPOSE 8080 3478/udp
ENTRYPOINT ["/usr/local/bin/meshvpn-server"]

# Relay Server Target
FROM alpine:3.18 AS relay
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/meshvpn-relay /usr/local/bin/meshvpn-relay
EXPOSE 41641/udp
ENTRYPOINT ["/usr/local/bin/meshvpn-relay"]
