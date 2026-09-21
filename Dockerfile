FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /git-mirrorer ./cmd/git-mirrorer

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
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
