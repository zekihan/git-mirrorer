FROM golang:1.23.3@sha256:26602182e32576fca9ad5397fd74b87ae68f7ee727436a85dd19a1c3766ad685 AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN ./scripts/build.sh

# Copy into scratch container
FROM golang:1.23.3-alpine@sha256:c694a4d291a13a9f9d94933395673494fc2cc9d4777b85df3a7e70b3492d3574
WORKDIR /

RUN apk add --no-cache ca-certificates git openssh-client

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tmp/bin/git-mirrorer ./

ENTRYPOINT ["./git-mirrorer"]
