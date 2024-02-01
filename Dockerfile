ARG ALPINE_VERSION=3.18
ARG GO_VERSION=1.21

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=SNAPSHOT
ENV CGO_ENABLED=0 GOOS=linux

WORKDIR /workspace

COPY go.mod go.sum /workspace/

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . /workspace/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go build -ldflags="-X 'github.com/movio/bramble.Version=$VERSION'" -o bramble ./cmd/bramble

FROM gcr.io/distroless/static

ARG VERSION=SNAPSHOT

LABEL org.opencontainers.image.title="Bramble"
LABEL org.opencontainers.image.description="A federated GraphQL API gateway"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.source="https://github.com/movio/bramble"
LABEL org.opencontainers.image.documentation="https://movio.github.io/bramble/"

COPY --from=builder /workspace/bramble .

EXPOSE 8082
EXPOSE 8083
EXPOSE 8084

ENTRYPOINT [ "/bramble" ]
