FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/credit-cabinet ./cmd/api

FROM golang:1.26-alpine AS goose
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM alpine:3.22 AS migrate
COPY --from=goose /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations
ENTRYPOINT ["goose"]

FROM alpine:3.22
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/credit-cabinet /app/credit-cabinet
COPY web /app/web
USER app
ENV HTTP_ADDR=:8080 WEB_DIR=/app/web
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/credit-cabinet"]
