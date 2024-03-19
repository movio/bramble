FROM golang:1.22-alpine3.19

ENV CGO_ENABLED=0

WORKDIR /go/src/app

COPY . .

RUN go generate .
RUN go get
CMD go run .
