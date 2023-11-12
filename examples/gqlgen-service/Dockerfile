FROM golang:1.20-alpine3.18

ENV CGO_ENABLED=0

WORKDIR /go/src/app

COPY . .

RUN go generate .
RUN go get
CMD go run .
