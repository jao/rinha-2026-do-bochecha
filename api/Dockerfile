FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o api .

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache wget ca-certificates && \
    mkdir -p /data && \
    wget -q "https://github.com/zanfranceschi/rinha-de-backend-2026/raw/main/resources/references.json.gz" \
         -O /data/references.json.gz && \
    apk del wget
COPY --from=builder /build/api .
CMD ["/app/api"]
