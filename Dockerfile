FROM golang:1.14-alpine as build
RUN apk add --no-cache git gcc musl-dev
ADD . /app
WORKDIR /app
RUN go build

FROM alpine:3.11
COPY --from=build /app/goshort /bin/
WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]