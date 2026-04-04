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

#ifndef SOL_TCP
#define SOL_TCP 6
#endif

#ifndef TCP_CONGESTION
#define TCP_CONGESTION 13
#endif

#ifndef TCP_WINDOW_CLAMP
#define TCP_WINDOW_CLAMP 10
#endif

#ifndef TCP_NODELAY
#define TCP_NODELAY 1
#endif

#ifndef TCP_USER_TIMEOUT
#define TCP_USER_TIMEOUT 18
#endif

#ifndef TCP_KEEPIDLE
#define TCP_KEEPIDLE 4
#endif

#ifndef TCP_BPF_IW
#define TCP_BPF_IW 1001
#endif

#ifndef TCP_BPF_SNDCWND_CLAMP
#define TCP_BPF_SNDCWND_CLAMP 1002
#endif


// Action definition (Tuning parameters for a target socket)
struct tuning_action {
    __u32 max_pacing_rate;
    __u32 snd_cwnd_clamp;
    __u32 cong_algo;
    __u32 init_cwnd;
    __u32 window_clamp;
    __u32 no_delay;
    __u32 rto_min;
    __u32 retrans_after;
    __u8  enable_ecn;
    __u8  pacing_status;
    __u32 keepalive_idle;
};

// Observation/Metrics tracked over a socket connection
struct tuning_metrics {
    __u32 srtt_us;
    __u32 total_retrans;
    __u64 bytes_sent;
    __u64 bytes_received;
    __u64 start_time_ns;
    __u64 duration_us;
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

    if (met->start_time_ns > 0) {
        met->duration_us = (bpf_ktime_get_ns() - met->start_time_ns) / 1000;
    }
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
                
                if (act->snd_cwnd_clamp > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_BPF_SNDCWND_CLAMP, &act->snd_cwnd_clamp, sizeof(act->snd_cwnd_clamp));
                }

                if (act->cong_algo > 0) {
                    if (act->cong_algo == 1) {
                        char cc[16] = "cubic";
                        bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, cc, sizeof(cc));
                    } else if (act->cong_algo == 2) {
                        char cc[16] = "bbr";
                        bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, cc, sizeof(cc));
                    }
                }

                if (act->init_cwnd > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_BPF_IW, &act->init_cwnd, sizeof(act->init_cwnd));
                }

                if (act->window_clamp > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_WINDOW_CLAMP, &act->window_clamp, sizeof(act->window_clamp));
                }

                if (act->no_delay > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_NODELAY, &act->no_delay, sizeof(act->no_delay));
                }

                if (act->retrans_after > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_USER_TIMEOUT, &act->retrans_after, sizeof(act->retrans_after));
                }

                if (act->keepalive_idle > 0) {
                    bpf_setsockopt(skops, SOL_TCP, TCP_KEEPIDLE, &act->keepalive_idle, sizeof(act->keepalive_idle));
                }
            }

            // Always track metrics to capture baselines as well
            struct tuning_metrics initial = {};
            initial.start_time_ns = bpf_ktime_get_ns();
            bpf_map_update_elem(&metrics_map, &key, &initial, BPF_ANY);
            
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
