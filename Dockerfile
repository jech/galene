FROM golang:1.13

WORKDIR /go/src/app
ADD . /go/src/app

RUN CGO_ENABLED=0 go build -ldflags='-s -w'

ENTRYPOINT [""]
CMD ["./galene", "&"]