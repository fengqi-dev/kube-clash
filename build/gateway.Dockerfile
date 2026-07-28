FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/kube-clash-gateway ./cmd/kube-clash-gateway
COPY internal/gateway ./internal/gateway
COPY internal/tunnel ./internal/tunnel
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kube-clash-gateway ./cmd/kube-clash-gateway

FROM scratch
COPY --from=build /out/kube-clash-gateway /kube-clash-gateway
USER 65532:65532
EXPOSE 1080
ENTRYPOINT ["/kube-clash-gateway"]
