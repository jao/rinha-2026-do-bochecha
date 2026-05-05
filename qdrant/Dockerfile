# Stage 1: Download reference data
FROM alpine:3.21 AS downloader
RUN apk add --no-cache wget && \
    wget -q "https://github.com/zanfranceschi/rinha-de-backend-2026/raw/main/resources/references.json.gz" \
         -O /references.json.gz

# Stage 2: Build the prebake binary (no CGO, no external deps)
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY prebake/go.mod ./
RUN go mod download
COPY prebake/*.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o prebake .

# Stage 3: Run Qdrant + prebake to build the HNSW index during docker build
FROM qdrant/qdrant:v1.13.4 AS indexer
USER root
COPY --from=builder /build/prebake /prebake
COPY --from=downloader /references.json.gz /data/references.json.gz
RUN ["/prebake"]
# Delete WAL segment files; keep dirs so Qdrant can init fresh WAL on startup
RUN find /qdrant/storage -path "*/wal/*" -type f -delete

# Final: clean Qdrant image with pre-built storage baked in
FROM qdrant/qdrant:v1.13.4
COPY --from=indexer /qdrant/storage /qdrant/storage
