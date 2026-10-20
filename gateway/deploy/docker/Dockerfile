FROM alpine:3.24.2
ARG BINARY_PATH
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -s /sbin/nologin yaginx \
    && install -d -o yaginx -g yaginx -m 0700 /var/lib/yaginx /var/log/yaginx
COPY ${BINARY_PATH} /usr/bin/yaginx
COPY --chown=yaginx:yaginx adminui/dist/ /usr/share/yaginx/adminui/
USER yaginx:yaginx
EXPOSE 8080 9080 8443 9443
EXPOSE 8443/udp
VOLUME ["/var/lib/yaginx"]
ENTRYPOINT ["/usr/bin/yaginx"]
CMD ["version"]
