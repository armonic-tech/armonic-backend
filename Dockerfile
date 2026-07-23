# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/armonic .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10001 armonic
USER armonic
WORKDIR /app

COPY --from=builder /out/armonic ./armonic

EXPOSE 8080
ENTRYPOINT ["./armonic"]
