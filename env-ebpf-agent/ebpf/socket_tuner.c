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
#ifndef SOL_TCP
#define SOL_TCP 6
#endif
#ifndef SO_MAX_PACING_RATE
#define SO_MAX_PACING_RATE 47
#endif
#ifndef TCP_CONGESTION
#define TCP_CONGESTION 13
#endif
#ifndef TCP_NODELAY
#define TCP_NODELAY 1
#endif
#ifndef TCP_WINDOW_CLAMP
#define TCP_WINDOW_CLAMP 10
#endif
#ifndef TCP_BPF_IW
#define TCP_BPF_IW 1001
#endif
#ifndef TCP_BPF_SNDCWND_CLAMP
#define TCP_BPF_SNDCWND_CLAMP 1002
#endif

struct tuning_action {
    __u32 max_pacing_rate;
    __u32 snd_cwnd_clamp;
    __u32 cong_algo;
    __u32 init_cwnd;
    __u32 window_clamp;
    __u32 no_delay;
};

struct tuning_metrics {
    __u32 srtt_us;
    __u32 total_retrans;
    __u64 bytes_sent;
    __u64 bytes_received;
    __u64 start_time_ns;
    __u64 duration_us;
    __u32 remote_ip4;
    __u32 remote_port;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, struct tuning_action);
} action_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, struct tuning_metrics);
} metrics_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192);
    __type(key, __u32);
    __type(value, __u32);
} infection_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192);
    __type(key, __u64);
    __type(value, __u32);
} cookie_target_map SEC(".maps");

static __always_inline void update_metrics(struct bpf_sock_ops *skops, __u32 target_pid, bool is_final) {
    struct tuning_metrics *met = bpf_map_lookup_elem(&metrics_map, &target_pid);
    if (!met) return;

    // Accumulate or update metrics
    // For srtt, we take the latest
    met->srtt_us = skops->srtt_us >> 3;
    met->total_retrans = skops->total_retrans;
    met->remote_ip4 = skops->remote_ip4;
    met->remote_port = skops->remote_port;

    // Use max to capture the total bytes seen across all sockets if they overlap?
    // Actually, for a single command, we just want the latest or sum.
    // Let's use simple assignment for now as it's a single metrics entry per command.
    met->bytes_sent = skops->bytes_acked;
    met->bytes_received = skops->bytes_received;

    if (met->start_time_ns > 0) {
        met->duration_us = (bpf_ktime_get_ns() - met->start_time_ns) / 1000;
    }
}

SEC("cgroup/connect4")
int bpf_connect4(struct bpf_sock_addr *ctx)
{
    if (ctx->family != AF_INET)
        return 1;

    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u32 target_pid = 0;

    __u32 *p_target = bpf_map_lookup_elem(&infection_map, &pid);
    if (p_target) {
        target_pid = *p_target;
    } else {
        if (bpf_map_lookup_elem(&action_map, &pid)) {
            target_pid = pid;
            bpf_map_update_elem(&infection_map, &pid, &target_pid, BPF_ANY);
        } else {
            struct task_struct *task = (struct task_struct *)bpf_get_current_task();
            struct task_struct *parent = BPF_CORE_READ(task, real_parent);
            __u32 ppid = BPF_CORE_READ(parent, tgid);
            __u32 *pp_target = bpf_map_lookup_elem(&infection_map, &ppid);
            if (!pp_target) {
               if (bpf_map_lookup_elem(&action_map, &ppid)) {
                   target_pid = ppid;
                   bpf_map_update_elem(&infection_map, &pid, &target_pid, BPF_ANY);
               } else {
                   struct task_struct *gparent = BPF_CORE_READ(parent, real_parent);
                   __u32 gppid = BPF_CORE_READ(gparent, tgid);
                   if (bpf_map_lookup_elem(&action_map, &gppid)) {
                       target_pid = gppid;
                       bpf_map_update_elem(&infection_map, &pid, &target_pid, BPF_ANY);
                   }
               }
            } else {
                target_pid = *pp_target;
                bpf_map_update_elem(&infection_map, &pid, &target_pid, BPF_ANY);
            }
        }
    }

    if (target_pid > 0) {
        __u64 cookie = bpf_get_socket_cookie(ctx);
        bpf_map_update_elem(&cookie_target_map, &cookie, &target_pid, BPF_ANY);

        struct tuning_metrics *met = bpf_map_lookup_elem(&metrics_map, &target_pid);
        if (!met) {
            struct tuning_metrics initial = {};
            initial.start_time_ns = bpf_ktime_get_ns();
            bpf_map_update_elem(&metrics_map, &target_pid, &initial, BPF_ANY);
        }
    }
    return 1;
}

SEC("sockops")
int bpf_sockmap(struct bpf_sock_ops *skops)
{
    __u32 op = skops->op;
    if (skops->family != AF_INET)
        return 0;

    __u64 cookie = bpf_get_socket_cookie(skops);
    __u32 *p_target_pid = bpf_map_lookup_elem(&cookie_target_map, &cookie);
    if (!p_target_pid)
        return 0;
    __u32 target_pid = *p_target_pid;

    switch (op) {
        case BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB:
        case BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB: {
            struct tuning_action *act = bpf_map_lookup_elem(&action_map, &target_pid);
            if (act) {
                if (act->max_pacing_rate > 0) bpf_setsockopt(skops, SOL_SOCKET, SO_MAX_PACING_RATE, &act->max_pacing_rate, sizeof(act->max_pacing_rate));
                if (act->snd_cwnd_clamp > 0) bpf_setsockopt(skops, SOL_TCP, TCP_BPF_SNDCWND_CLAMP, &act->snd_cwnd_clamp, sizeof(act->snd_cwnd_clamp));
                if (act->init_cwnd > 0) bpf_setsockopt(skops, SOL_TCP, TCP_BPF_IW, &act->init_cwnd, sizeof(act->init_cwnd));
                if (act->window_clamp > 0) bpf_setsockopt(skops, SOL_TCP, TCP_WINDOW_CLAMP, &act->window_clamp, sizeof(act->window_clamp));
                if (act->no_delay > 0) { __u32 val = 1; bpf_setsockopt(skops, SOL_TCP, TCP_NODELAY, &val, sizeof(val)); }
                if (act->cong_algo == 1) { char c[] = "cubic"; bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, c, sizeof(c)); }
                else if (act->cong_algo == 2) { char c[] = "bbr"; bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, c, sizeof(c)); }
                else if (act->cong_algo == 3) { char c[] = "reno"; bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, c, sizeof(c)); }
            }
            update_metrics(skops, target_pid, false);
            bpf_sock_ops_cb_flags_set(skops, skops->bpf_sock_ops_cb_flags | BPF_SOCK_OPS_STATE_CB_FLAG | BPF_SOCK_OPS_RETRANS_CB_FLAG);
            break;
        }
        case BPF_SOCK_OPS_RETRANS_CB:
        case BPF_SOCK_OPS_STATE_CB: {
            update_metrics(skops, target_pid, true);
            if (op == BPF_SOCK_OPS_STATE_CB) {
                bpf_map_delete_elem(&cookie_target_map, &cookie);
            }
            break;
        }
    }
    return 0;
}

char _license[] SEC("license") = "GPL";
