FROM golang:1.23-rc-alpine AS build

WORKDIR /app

COPY ../../go.mod ../../go.sum ./

RUN go mod download

COPY ../../ .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o api ./cmd/api/

FROM alpine:latest

RUN apk --no-cache add ca-certificates

COPY --from=build /app/api /api

EXPOSE 3001

CMD ["/api"]