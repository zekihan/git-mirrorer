FROM --platform=$BUILDPLATFORM alpine:3.23.2@sha256:865b95f46d98cf867a156fe4a135ad3fe50d2056aa3f25ed31662dff6da4eb62 AS certs
RUN apk update && apk add ca-certificates

FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:b86c79d6d337dcaa4cad3cfc704d5e6ae5ba725f3e0674d96f65af7a8f5e2761 AS builder

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

FROM golang:1.25.6-alpine@sha256:d9b2e14101f27ec8d09674cd01186798d227bb0daec90e032aeb1cd22ac0f029 AS final

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
