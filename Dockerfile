FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/promptify-server ./cmd/server

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app \
    && mkdir -p /app/data \
    && chown -R app:app /app

WORKDIR /app

COPY --from=builder /out/promptify-server ./promptify-server
COPY web ./web

EXPOSE 8080

USER app

ENTRYPOINT ["./promptify-server"]
