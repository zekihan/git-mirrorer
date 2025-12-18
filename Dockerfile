FROM --platform=$BUILDPLATFORM alpine:3.23.2@sha256:865b95f46d98cf867a156fe4a135ad3fe50d2056aa3f25ed31662dff6da4eb62 AS certs
RUN apk update && apk add ca-certificates

FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:d80cd694d3e9467884fcb94b8ca1e20437d8a501096cdf367a5a1918a34fc2fd AS builder

ARG TARGETPLATFORM

COPY ./dist ./dist

RUN case ${TARGETPLATFORM} in \
        "linux/amd64")    ARCH="amd64"    ;; \
        "linux/arm64")    ARCH="arm64"    ;; \
        "linux/ppc64le")  ARCH="ppc64le"  ;; \
        "linux/s390x")    ARCH="s390x"    ;; \
        "linux/386")      ARCH="386"      ;; \
        "linux/arm/v7")   ARCH="armv7"   ;; \
        "linux/arm/v6")   ARCH="armv6"   ;; \
    esac && \
    mv ./dist/git-mirrorer_linux_${ARCH}/git-mirrorer /usr/bin/git-mirrorer

FROM golang:1.25.5-alpine@sha256:26111811bc967321e7b6f852e914d14bede324cd1accb7f81811929a6a57fea9 AS final

RUN adduser -D -u 25090 -h /home/git-mirrorer git-mirrorer

WORKDIR /home/git-mirrorer

COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=builder /usr/bin/git-mirrorer /usr/bin/git-mirrorer

RUN apk add --no-cache ca-certificates git openssh-client

USER git-mirrorer

ENTRYPOINT ["git-mirrorer"]

LABEL org.opencontainers.image.source="https://github.com/zekihan/git-mirrorer" \
    org.opencontainers.image.licenses="AGPL-3.0" \
    org.opencontainers.image.title="Git Mirrorer" \
    org.opencontainers.image.description="Git Mirrorer is a tool to mirror your git repositories to multiple platforms."
