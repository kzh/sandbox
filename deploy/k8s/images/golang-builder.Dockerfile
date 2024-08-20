FROM golang:1.23-bookworm

RUN go telemetry off

WORKDIR /app

RUN go mod init sandbox

RUN GOOS=linux GOARCH=amd64 go build std