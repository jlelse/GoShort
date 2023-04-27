FROM golang:1.20-alpine3.17 as build
WORKDIR /app
ENV CGO_ENABLED=0
ADD *.go go.mod go.sum /app/
ADD templates/ /app/templates/
RUN go test -cover ./...
RUN go build -ldflags '-w -s' -o goshort

FROM alpine:3.17
WORKDIR /app
VOLUME /app/config
VOLUME /app/data
EXPOSE 8080
CMD ["goshort"]
COPY --from=build /app/goshort /bin/