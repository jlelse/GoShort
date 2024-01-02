FROM golang:1.21-alpine3.19 as buildbase

WORKDIR /app
ENV CGO_ENABLED=0
ADD *.go go.mod go.sum /app/
ADD templates/ /app/templates/

FROM buildbase AS build

RUN go build -ldflags '-w -s' -o goshort

FROM build AS test

RUN go test -timeout 300s -failfast -cover ./...

FROM alpine:3.19 AS base

WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]
COPY --from=build /app/goshort /bin/