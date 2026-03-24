FROM --platform=$BUILDPLATFORM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS certs
RUN apk update && apk add ca-certificates

FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:1487d0af5f52b4ba31c7e465126ee2123fe3f2305d638e7827681e7cf6c83d5e AS builder

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

FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS final

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
