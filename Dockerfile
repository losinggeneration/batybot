FROM golang:1.25-alpine as builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED 0
RUN go generate ./...
RUN go build -o /srv/bot .

FROM alpine:3.22

RUN adduser -D go
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

WORKDIR /srv
COPY --from=builder /srv .

USER go

EXPOSE "8080"
ENTRYPOINT ["./bot"]
