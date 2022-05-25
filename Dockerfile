FROM golang:1.18-alpine as builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED 0
RUN go generate ./...
RUN go build -o /srv/bot .

FROM alpine:3.10

RUN adduser -D go
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

WORKDIR /srv
COPY --from=builder /srv .

USER go

ENTRYPOINT ["./bot"]
