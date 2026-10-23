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
    mkdir -p /app/data && chown worklog:worklog /app/data && chmod 700 /app/data
WORKDIR /app
COPY --from=backend /worklog /app/worklog
COPY --from=web /src/Web/dist/ /app/web/
ENV WORKLOG_ADDR=0.0.0.0:8080 \
    WORKLOG_DB_DSN=/app/data/worklog.db \
    WORKLOG_TOKEN_FILE=/app/data/mcp-token \
    WORKLOG_STATIC_DIR=/app/web
USER 10001:10001
VOLUME ["/app/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/worklog"]
CMD ["serve"]
