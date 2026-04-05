# --- BUILD STAGE ---
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y \
    protobuf-compiler \
    make

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make generate
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/env-router main.go

# --- RUNTIME STAGE ---
FROM ubuntu:24.04

WORKDIR /app

COPY --from=builder /app/bin/env-router /usr/local/bin/

# Expose standard router port
EXPOSE 50050

# Configuration is provided via environment variables:
#   AGENT_SERVICE_NAME      (required) - K8s service name for env-ebpf-agents
#   AGENT_DEPLOYMENT_NAME   (required) - K8s deployment name to scale
#   AGENT_NAMESPACE         (default: "default")
#   AGENT_GRPC_PORT         (default: "50051")
#   WARM_POOL_SIZE          (default: "1")
#   SCALER_INTERVAL_SECONDS (default: "10")
#   LISTEN_ADDR             (default: ":50050")
ENTRYPOINT ["env-router"]
