FROM golang:1.23.3@sha256:d56c3e08fe5b27729ee3834854ae8f7015af48fd651cd25d1e3bcf3c19830174 AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN ./scripts/build.sh

# Copy into scratch container
FROM golang:1.23.3-alpine@sha256:c5eb6fec061d27450aca04855e80d4c78b745c94db1431f8ea5459f56fc24577
WORKDIR /

RUN apk add --no-cache ca-certificates git openssh-client

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tmp/bin/git-mirrorer ./

ENTRYPOINT ["./git-mirrorer"]
