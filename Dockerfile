FROM golang:1.14-alpine3.12 as build
RUN apk add --no-cache git gcc musl-dev sqlite-dev
ADD . /app
WORKDIR /app
RUN go build --tags "libsqlite3 linux"

FROM alpine:3.12
RUN apk add --no-cache sqlite-dev
COPY --from=build /app/goshort /bin/
WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]