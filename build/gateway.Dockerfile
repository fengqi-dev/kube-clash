FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/kubeloop-gateway ./cmd/kubeloop-gateway
COPY internal/gateway ./internal/gateway
COPY internal/tunnel ./internal/tunnel
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kube-loop-gateway ./cmd/kubeloop-gateway

FROM scratch
COPY --from=build /out/kube-loop-gateway /kube-loop-gateway
USER 65532:65532
EXPOSE 1080
ENTRYPOINT ["/kube-loop-gateway"]
