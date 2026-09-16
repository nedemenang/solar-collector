FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/collector ./cmd/collector

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/collector /collector
COPY config/config.yaml /config/config.yaml
ENTRYPOINT ["/collector", "-config", "/config/config.yaml"]
