FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/tracker ./cmd/tracker/main.go

RUN go build -o /app/client ./client/


FROM alpine:latest

COPY --from=builder /app/tracker /usr/local/bin/tracker
COPY --from=builder /app/client /usr/local/bin/client

RUN chmod +x /usr/local/bin/tracker
RUN chmod +x /usr/local/bin/client
