# Local/CI e2e image. Expects a prebuilt Linux binary at build/bin/kube-loop-gateway.
FROM alpine:3.22 AS perms
COPY build/bin/kube-loop-gateway /kube-loop-gateway
RUN chmod 755 /kube-loop-gateway

FROM scratch
COPY --from=perms /kube-loop-gateway /kube-loop-gateway
USER 65532:65532
EXPOSE 1080
ENTRYPOINT ["/kube-loop-gateway"]
