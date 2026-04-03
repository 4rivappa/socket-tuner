# --- BUILD STAGE ---
FROM golang:1.22-bullseye AS builder

RUN apt-get update && apt-get install -y \
    protobuf-compiler \
    make

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make generate
RUN CGO_ENABLED=0 GoOS=linux go build -o bin/env-router main.go

# --- RUNTIME STAGE ---
FROM ubuntu:24.04

WORKDIR /app

COPY --from=builder /app/bin/env-router /usr/local/bin/

# Expose standard router port
EXPOSE 50050

# In Kubernetes, you will pass the agent comma-separated list of addresses via arguments
ENTRYPOINT ["env-router", "-listen", ":50050"]
