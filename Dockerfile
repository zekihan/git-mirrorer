FROM golang:1.22.6 AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN ./build.sh

# Copy into scratch container
FROM golang:1.22.6-alpine
WORKDIR /

RUN apk add --no-cache git ca-certificates openssh-client

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tmp/bin/git-mirrorer ./

ENTRYPOINT ["./git-mirrorer"]
