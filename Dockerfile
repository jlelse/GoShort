FROM golang:1.17-alpine3.14 as build
WORKDIR /app
RUN apk add --no-cache git gcc musl-dev
ADD *.go go.mod go.sum /app/
ADD templates/ /app/templates/
RUN go test -cover ./...
RUN go build -ldflags '-w -s' -o goshort

FROM alpine:3.14
WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]
COPY --from=build /app/goshort /bin/