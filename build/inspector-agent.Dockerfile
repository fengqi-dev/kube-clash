FROM golang:1.26-alpine AS build
ARG VERSION=dev
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/kubeloop-inspector-agent ./cmd/kubeloop-inspector-agent
COPY internal/gateway ./internal/gateway
COPY internal/inspectoragent ./internal/inspectoragent
COPY internal/tunnel ./internal/tunnel
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o /out/kube-loop-inspector-agent ./cmd/kubeloop-inspector-agent

FROM scratch
ARG VERSION=dev
LABEL org.opencontainers.image.title="KubeLoop Inspector Agent" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/kube-loop-inspector-agent /kube-loop-inspector-agent
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/kube-loop-inspector-agent"]
