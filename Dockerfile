FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /git-mirrorer ./cmd/git-mirrorer

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add --no-cache ca-certificates git git-lfs openssh-client \
    && adduser -D -u 25090 git-mirrorer \
    && mkdir -p /data/.git-mirrorer \
    && chown -R git-mirrorer:git-mirrorer /data
COPY --from=build /git-mirrorer /usr/local/bin/git-mirrorer
USER git-mirrorer
WORKDIR /data
ENTRYPOINT ["git-mirrorer"]
CMD ["--config", "/data/git-mirrorer.yaml"]
LABEL org.opencontainers.image.source="https://github.com/zekihan/git-mirrorer" \
      org.opencontainers.image.licenses="AGPL-3.0" \
      org.opencontainers.image.title="Git Mirrorer" \
      org.opencontainers.image.description="One-shot replication of Git repositories"
