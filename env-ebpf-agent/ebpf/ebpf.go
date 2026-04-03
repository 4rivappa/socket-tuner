package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpf Bpf socket_tuner.c -- -I../ebpf -O2 -Wall
