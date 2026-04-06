# Multi-stage build for the apix-engine binary
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /apix-engine ./cmd/apix-engine/

# Runtime image
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /apix-engine /usr/local/bin/apix-engine

EXPOSE 8080 9090
VOLUME /data

ENV APIX_DB_PATH=/data/apix.db
ENV APIX_CA_CERT_PATH=/data/ca.pem
ENV APIX_CA_KEY_PATH=/data/ca-key.pem

ENTRYPOINT ["apix-engine"]
