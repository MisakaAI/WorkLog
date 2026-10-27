FROM node:24-alpine AS web
WORKDIR /src/Web
COPY Web/package*.json ./
RUN npm ci
COPY Web/ ./
RUN npm run build

FROM golang:1.27.3-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /worklog ./cmd/worklog

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && \
    addgroup -g 10001 worklog && adduser -D -u 10001 -G worklog worklog && \
    mkdir -p /app/data /app/config && chown worklog:worklog /app/data && chmod 700 /app/data
WORKDIR /app
COPY --from=backend /worklog /app/worklog
COPY --from=web /src/Web/dist/ /app/web/
# Non-sensitive defaults intentionally fail remote-bind validation until the
# operator mounts a JSON file containing independent credentials.
COPY config.docker.example.json /app/config/config.json
USER 10001:10001
VOLUME ["/app/data"]
EXPOSE 12450
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:12450/healthz || exit 1
ENTRYPOINT ["/app/worklog"]
CMD ["--conf", "/app/config/config.json", "serve"]
