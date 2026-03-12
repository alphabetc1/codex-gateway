FROM golang:1.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/claude-gateway ./cmd/claude-gateway

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=build /out/claude-gateway /usr/local/bin/claude-gateway

EXPOSE 8080 9090

ENTRYPOINT ["/usr/local/bin/claude-gateway"]
