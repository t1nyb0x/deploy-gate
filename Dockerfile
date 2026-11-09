FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o deploy-gate \
    ./cmd/deploy-gate

FROM alpine:3.22

RUN adduser -D -u 10001 app

USER app

COPY --from=builder /app/deploy-gate /usr/local/bin/deploy-gate

ENTRYPOINT ["deploy-gate"]