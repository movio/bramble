FROM golang:1.17 AS builder

ARG VERSION=SNAPSHOT
ENV GO111MODULE=on

WORKDIR /workspace

COPY go.mod go.sum /workspace/

RUN go mod download

COPY *.go /workspace/
COPY cmd /workspace/cmd
COPY plugins /workspace/plugins

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X 'github.com/movio/bramble.Version=$VERSION'" -o bramble ./cmd/bramble

FROM gcr.io/distroless/static

COPY --from=builder /workspace/bramble .

EXPOSE 8082
EXPOSE 8083
EXPOSE 8084

CMD [ "/bramble", "-conf", "/config.json" ]
