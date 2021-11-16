# Build de l'executable galene
FROM golang:1.13
WORKDIR /app/galene
COPY ./galene /app/galene
RUN mkdir groups
COPY ./docker_groups.sh .
RUN sh ./docker_groups.sh && rm -f docker_groups.sh
RUN CGO_ENABLED=0 go build -ldflags='-s -w'
ENTRYPOINT ["/app/galene"]

# Build de l'image galene
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /app/galene .
CMD ["./galene"]
