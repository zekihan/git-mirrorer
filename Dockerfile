FROM golang:1.22.2-alpine AS builder

ARG GITHUB_TOKEN

# Setup
RUN mkdir -p /go/src/github.com/zekihan/git-mirrorer
WORKDIR /go/src/github.com/zekihan/git-mirrorer

RUN apk add --no-cache git ca-certificates make

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN make build

# Copy into scratch container
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/github.com/zekihan/git-mirrorer/tmp/bin/git-mirrorer ./

ENTRYPOINT ["./git-mirrorer"]
