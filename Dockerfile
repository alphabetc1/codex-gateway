FROM --platform=$BUILDPLATFORM golang:1.24 AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/codex-gateway ./cmd/codex-gateway

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=build /out/codex-gateway /usr/local/bin/codex-gateway

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/codex-gateway"]
