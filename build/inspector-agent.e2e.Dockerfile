# Local/CI e2e image. Expects a prebuilt Linux binary at build/bin/kube-loop-inspector-agent.
FROM scratch
USER 65532:65532
COPY build/bin/kube-loop-inspector-agent /kube-loop-inspector-agent
ENTRYPOINT ["/kube-loop-inspector-agent"]
