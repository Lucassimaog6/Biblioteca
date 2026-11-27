FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/biblioteca ./cmd/biblioteca

FROM alpine:3.22

RUN mkdir -p /data && chown 65532:65532 /data
COPY --from=build --chown=65532:65532 /out/biblioteca /usr/local/bin/biblioteca

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/biblioteca"]
