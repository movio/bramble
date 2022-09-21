FROM golang:1.17-alpine3.15

ENV CGO_ENABLED=0

WORKDIR /go/src/app

COPY . .

RUN go generate .
RUN go get
CMD go run .
