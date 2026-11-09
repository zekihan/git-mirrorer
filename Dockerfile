FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /git-mirrorer ./cmd/git-mirrorer

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache ca-certificates git git-lfs openssh-client tini \
    && addgroup -g 25090 git-mirrorer \
    && adduser -D -u 25090 -G git-mirrorer -h /home/git-mirrorer git-mirrorer \
    && mkdir -p /data/.git-mirrorer \
    && chown -R 25090:25090 /data
COPY --from=build /git-mirrorer /usr/local/bin/git-mirrorer
USER 25090:25090
WORKDIR /data
# git-mirrorer kills timed-out Git process groups; tini reaps helpers orphaned by them.
ENTRYPOINT ["/sbin/tini", "--", "git-mirrorer"]
CMD ["--config", "/data/git-mirrorer.yaml"]
LABEL org.opencontainers.image.source="https://github.com/zekihan/git-mirrorer" \
      org.opencontainers.image.licenses="AGPL-3.0" \
      org.opencontainers.image.title="Git Mirrorer" \
      org.opencontainers.image.description="One-shot replication of Git repositories"
