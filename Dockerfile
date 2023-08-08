FROM golang:1.20-alpine3.18 AS builder

ARG VERSION=SNAPSHOT
ENV CGO_ENABLED=0 GOOS=linux

WORKDIR /workspace

COPY go.mod go.sum /workspace/

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . /workspace/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go build -ldflags="-X 'github.com/movio/bramble.Version=$VERSION'" -o bramble ./cmd/bramble

FROM gcr.io/distroless/static

LABEL org.opencontainers.image.source="https://github.com/movio/bramble"

COPY --from=builder /workspace/bramble .

EXPOSE 8082
EXPOSE 8083
EXPOSE 8084

ENTRYPOINT [ "/bramble" ]
