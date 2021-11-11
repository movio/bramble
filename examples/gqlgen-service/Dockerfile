FROM golang

WORKDIR /go/src/app

COPY . .

RUN go generate .
RUN go get
CMD go run .
