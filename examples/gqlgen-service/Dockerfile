FROM golang:1.19-alpine3.16

ENV CGO_ENABLED=0

WORKDIR /go/src/app

COPY . .

RUN go generate .
RUN go get
CMD go run .
