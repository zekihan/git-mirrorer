FROM --platform=$BUILDPLATFORM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS certs
RUN apk update && apk add ca-certificates

FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:b3255e7dfbcd10cb367af0d409747d511aeb66dfac98cf30e97e87e4207dd76f AS builder

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

FROM golang:1.25.7-alpine@sha256:81d49e1de26fa223b9ae0b4d5a4065ff8176a7d80aa5ef0bd9f2eee430afe4d7 AS final

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
