FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bridge ./cmd/bridge

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates gosu \
    && rm -rf /var/lib/apt/lists/*
RUN useradd --create-home --uid 1000 app
WORKDIR /app
COPY --from=build /out/bridge /app/bridge
COPY deploy/docker/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh && mkdir -p /app/data && chown -R app:app /app/data
ENV ORDERPY_KEY_PATH=/app/data/bridge_key.pem
ENV ORDERPY_BRIDGE_VARIANT=docker
EXPOSE 8080 8088
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/bridge"]
