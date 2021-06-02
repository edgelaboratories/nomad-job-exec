FROM alpine:3.6
RUN apk update && \
    apk add ca-certificates

ADD bin/nomad-alloc-exec /
ENTRYPOINT ["/nomad-alloc-exec"]
