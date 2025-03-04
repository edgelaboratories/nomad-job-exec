FROM alpine
RUN apk update && \
    apk add ca-certificates

COPY nomad-job-exec /nomad-job-exec

ENTRYPOINT ["/nomad-job-exec"]
