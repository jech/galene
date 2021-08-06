FROM golang:1.13

WORKDIR /go/src/galene
COPY . /go/src/galene

RUN mkdir groups
RUN sh DOCKER/docker_groups.sh && rm -rf DOCKER/

RUN CGO_ENABLED=0 go build -ldflags='-s -w'

ENTRYPOINT [""]
CMD ["./galene", "&"]