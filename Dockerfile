FROM --platform=$BUILDPLATFORM alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4 AS certs
RUN apk update && apk add ca-certificates

FROM --platform=$BUILDPLATFORM busybox:1.38.0@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS builder

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

FROM golang:1.26.4-alpine@sha256:bd14630652464086289693533d25b791aa9ae7481e784d7eac5d4c948e9736ea AS final

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
