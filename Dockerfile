FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/stockwatcher ./cmd/stockwatcher

FROM alpine:3.22
RUN apk add --no-cache ca-certificates su-exec \
    && adduser -D -u 65532 app
COPY --from=build /out/stockwatcher /usr/local/bin/stockwatcher
COPY docker-entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh && chmod +x /entrypoint.sh
WORKDIR /data
VOLUME /data
ENV DB_PATH=/data/stockwatcher.db \
    ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
CMD ["serve"]
