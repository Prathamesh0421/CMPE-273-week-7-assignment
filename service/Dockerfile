## Stage 1: Build
FROM golang:1.22-alpine AS builder

WORKDIR /build

COPY go.mod .
RUN go mod download

COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -o service .

## Stage 2: Runtime
FROM alpine:3.19

RUN apk add --no-cache wget

COPY --from=builder /build/service /service

ENTRYPOINT ["/service"]
