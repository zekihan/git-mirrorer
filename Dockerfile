FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /git-mirrorer ./cmd/git-mirrorer

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
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
