# --- BUILD STAGE ---
FROM golang:1.22-bullseye AS builder

# Install necessary build tools for eBPF and gRPC compilation
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    make \
    protobuf-compiler \
    libbpf-dev

# Install required Go protocol buffer plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

WORKDIR /app

# Cache modules first
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire agent codebase
COPY . .

# Generate the Protobuf stubs and eBPF Go bindings
RUN make generate

# Build the final static binary
RUN CGO_ENABLED=0 go build -o bin/env-ebpf-agent main.go

# --- RUNTIME STAGE ---
# We use Ubuntu as the base runtime so standard networking baseline tools exist for the Executor
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    curl \
    git \
    wget \
    iproute2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/bin/env-ebpf-agent /usr/local/bin/

# Expose gRPC port
EXPOSE 50051

# Requires privileged execution in Kubernetes to attach to /sys/fs/cgroup
ENTRYPOINT ["env-ebpf-agent", "-listen", ":50051", "-cgroup", "/sys/fs/cgroup"]
