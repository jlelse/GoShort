FROM golang:1.16-alpine3.14 as build
WORKDIR /app
ENV GOFLAGS="-tags=linux,libsqlite3"
RUN apk add --no-cache git gcc musl-dev
RUN apk add --no-cache --repository=http://dl-cdn.alpinelinux.org/alpine/edge/main sqlite-dev
ADD . /app
RUN go test -cover ./...
RUN go build -ldflags '-w -s' -o goshort

FROM alpine:3.14
WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]
RUN apk add --no-cache --repository=http://dl-cdn.alpinelinux.org/alpine/edge/main sqlite-dev
COPY --from=build /app/goshort /bin/