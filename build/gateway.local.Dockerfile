FROM scratch
COPY build/bin/kube-clash-gateway-linux-arm64 /kube-clash-gateway
USER 65532:65532
EXPOSE 1080
ENTRYPOINT ["/kube-clash-gateway"]
