.PHONY: generate all build

export PATH := $(PATH):$(HOME)/go/bin

all: build

generate:
	# Keep directory clean
	mkdir -p pkg/pb

	# Compile protobuf
	protoc --go_out=pkg/pb --go_opt=paths=source_relative \
	    --go-grpc_out=pkg/pb --go-grpc_opt=paths=source_relative \
	    -I proto proto/agent.proto
	
	# Compile eBPF
	cd ebpf && go generate

build: generate
	go build -o bin/env-ebpf-agent main.go
