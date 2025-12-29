FROM golang:1.25-alpine3.23 as buildbase

WORKDIR /app
ENV CGO_ENABLED=0
ADD *.go go.mod go.sum /app/
ADD templates/ /app/templates/
ADD static/ /app/static/

FROM buildbase AS build

RUN go build -ldflags '-w -s' -o goshort

FROM build AS test

RUN go test -timeout 300s -failfast -cover ./...

FROM scratch AS base

WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
COPY --from=build /app/goshort /goshort
CMD ["/goshort"]