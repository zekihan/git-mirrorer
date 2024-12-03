FROM golang:1.23.3@sha256:b4aabba13d38c069fa6d6ee3656e1fcb11c18181ecfd13728bfced414f6422ca AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN ./scripts/build.sh

# Copy into scratch container
FROM golang:1.23.3-alpine@sha256:c694a4d291a13a9f9d94933395673494fc2cc9d4777b85df3a7e70b3492d3574 AS final

RUN adduser -D -u 25090 -h /home/git-mirrorer git-mirrorer

WORKDIR /home/git-mirrorer

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tmp/bin/git-mirrorer ./

RUN apk add --no-cache ca-certificates git openssh-client

USER git-mirrorer

ENTRYPOINT ["./git-mirrorer"]
