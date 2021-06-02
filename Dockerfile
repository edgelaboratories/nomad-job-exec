FROM alpine
RUN apk update && \
    apk add ca-certificates

ADD bin/nomad-job-exec /
ENTRYPOINT ["/nomad-job-exec"]
