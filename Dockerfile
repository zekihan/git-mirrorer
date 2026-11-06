FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:dbaa054703263d06c8b49576667418473b6231f8c0fa69dee103207fdb8e3654 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /git-mirrorer ./cmd/git-mirrorer

FROM alpine:3.24.4@sha256:e2944c2947c8cabdd6b23a2ebcf8094a9f3d53ca9e0340edd065237ab74c18c0
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
