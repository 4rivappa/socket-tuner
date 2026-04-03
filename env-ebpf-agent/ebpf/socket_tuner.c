//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

#ifndef AF_INET
#define AF_INET 2
#endif

#ifndef SOL_SOCKET
#define SOL_SOCKET 1
#endif

#ifndef SO_MAX_PACING_RATE
#define SO_MAX_PACING_RATE 47 // Defined in asm-generic/socket.h usually
#endif

// Action definition (Tuning parameters for a target socket)
struct tuning_action {
    __u32 max_pacing_rate;
    __u32 snd_cwnd_clamp;
};

// Observation/Metrics tracked over a socket connection
struct tuning_metrics {
    __u32 srtt_us;
    __u32 total_retrans;
    __u64 bytes_sent;
    __u64 bytes_received;
};

// Map: Target IP (IPv4) : Port (v4) -> tuning_action
struct ip_port_key {
    __be32 ip;
    __be32 port;
};

// BPF map to store the tuning limits injected by RL
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, struct ip_port_key);
    __type(value, struct tuning_action);
} action_map SEC(".maps");

// BPF map to store outcome metrics to be polled by the Agent
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, struct ip_port_key);
    __type(value, struct tuning_metrics);
} metrics_map SEC(".maps");

static __always_inline void update_metrics(struct bpf_sock_ops *skops) {
    struct ip_port_key key = {};
    key.ip = skops->remote_ip4;
    key.port = 0; // Ignore port

    struct tuning_metrics *met = bpf_map_lookup_elem(&metrics_map, &key);
    if (!met) {
        return;
    }

    met->srtt_us = skops->srtt_us >> 3; // Linux srtt scaling
    met->total_retrans = skops->total_retrans;
    met->bytes_sent = skops->bytes_acked;
    met->bytes_received = skops->bytes_received;
}

SEC("sockops")
int bpf_sockmap(struct bpf_sock_ops *skops)
{
    __u32 op = skops->op;
    
    // Only care about IPv4 for now
    if (skops->family != AF_INET)
        return 0;

    struct ip_port_key key = {};
    key.ip = skops->remote_ip4;
    key.port = 0; // Ignore port

    switch (op) {
        case BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB:
        case BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB: {
            // Socket established, let's see if we have an action for it.
            struct tuning_action *act = bpf_map_lookup_elem(&action_map, &key);
            if (act) {
                if (act->max_pacing_rate > 0) {
                    bpf_setsockopt(skops, SOL_SOCKET, SO_MAX_PACING_RATE, &act->max_pacing_rate, sizeof(act->max_pacing_rate));
                }

                struct tuning_metrics initial = {};
                bpf_map_update_elem(&metrics_map, &key, &initial, BPF_ANY);
            }
            // Enable callbacks for state changes (e.g. TCP close) to grab final stats
            bpf_sock_ops_cb_flags_set(skops, skops->bpf_sock_ops_cb_flags | BPF_SOCK_OPS_STATE_CB_FLAG);
            break;
        }
        case BPF_SOCK_OPS_STATE_CB: {
            // TCP State machine change. 
            // args[1] contains old state. skops->state contains new state?
            // Usually we capture on any state change we requested (we just grab latest config)
            update_metrics(skops);
            break;
        }
        case BPF_SOCK_OPS_RETRANS_CB: {
            // Also update on retransmissions if we wanted to set that flag
            update_metrics(skops);
            break;
        }
    }
    return 0;
}

char _license[] SEC("license") = "GPL";
