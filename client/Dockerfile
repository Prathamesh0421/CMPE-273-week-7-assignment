## Stage 1: Build
FROM golang:1.22-alpine AS builder

WORKDIR /build

COPY go.mod .
RUN go mod download

COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -o client .

## Stage 2: Runtime
FROM alpine:3.19

COPY --from=builder /build/client /client

ENTRYPOINT ["/client"]
