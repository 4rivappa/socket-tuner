## eBPF based linux kernel network socket tuner

### Training Environment
In EKS Kubernetes cluster, we will be hosting few nodes.
Each node contains eBPF agent and environment agent. 
One node will have a environment router, which will be responsible to route the traffic to the environment node agent.
Environment agent will be responsible to generate traffic to the environment.

RL Inference <-> EKS Cluster (environment router <-> environment agent <-> eBPF agent)
This is the overall flow.

### RL Environment Steps

reset()
step()
getstate()

reset() -> creates a new scenario, with specific network-command to be executed (like closing a git repo, download s3, reach some CDN, etc)
at this stage, the new request will be sent to environment router. router then assigns some environment agent (which is free to execute loop).
this environment agent gets information about network-command to be executed, and it will execute it without any socket tuning at first.
returns the initial observation of that network call like below (remote ip, remote port, srtt, mdev, total retrans, bytes sent/received)

step(action) -> action will be sent to environment agent, which runs this action (tuning the socket parameter).
that means this environment agent stores the configuration for that remote IP into an eBPF map - which will be read by eBPF agent to tune the next socket initialization.
after loading this socket tuning parameter into eBPF maps,
environment agent will re-trigger the network-command - which will be intercepted by our eBPF agent this time.
the new configuration network-call will be closed and that will be recorded in ebpf agent, 
new observation about this call will be reported back to environment agent, which then gives back to RL inference engine.
at which LLM as a judge will determine the reward for this step, based on that RL inference engine will be updated respectively.

these steps repeat for some time.
until MAX_STEPS is reached (like 4,5) then it is stoped.

new episode will be created with reset.


### eBPF Agent

1. Observations on TCP socket connection closes, so that we can know - how socket tuning is working.
Below are the example parameters which we are looking for - tuning the socket configuration.

```english
struct socket_event {
    __u32 remote_ip;
    __u32 remote_port;
    __u32 srtt_us;        // Final smoothed RTT
    __u32 mdev_us;        // RTT variance (jitter)
    __u32 total_retrans;  // Packet loss indicator
    __u64 bytes_sent / receieved;     // To distinguish "Elephant" vs "Mice" flows
};
```

2. Intercepts all new Socket creations in TCP connection state like below, and update the socket configuration.
using socket_ops hook or similar, we can intercept the socket creation and update the socket configuration.

```english
// Define the "Tuning Policy" for a specific IP or remote IP and port
struct socket_config {
    char cc[16];          // Congestion Control name (e.g., "bbr", "cubic")
    int window_clamp;     // Max advertised window (e.g., 16777216)
    int init_cwnd;        // Initial congestion window (e.g., 20 or 50)
    int nodelay;          // 1 to disable Nagle's algorithm
};

// The Map: Key is the Remote IP, Value is the Policy
struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE); // Use LPM for subnet-based policies
    __uint(max_entries, 65536);
    __type(key, struct bpf_lpm_trie_key_u32); // Prefix length + IP
    __type(value, struct socket_config);
    __uint(map_flags, BPF_F_NO_PREALLOC);
} llm_policy_map SEC(".maps");

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

SEC("sock_ops")
int bpf_tune_on_init(struct bpf_sock_ops *skops) {
    // We only care about the moment the connection is being initialized
    if (skops->op != BPF_SOCK_OPS_TCP_CONNECT_CB) {
        return 1;
    }

    // 1. Prepare the lookup key for the remote IP
    struct bpf_lpm_trie_key_u32 key = {
        .prefixlen = 32,
        .data = skops->remote_ip4 // The destination IP
    };

    // 2. Perform the high-speed lookup
    struct socket_config *conf = bpf_map_lookup_elem(&llm_policy_map, &key);

    // 3. Logic: If NOT present, do nothing (Kernel defaults stay)
    if (!conf) {
        return 1; 
    }

    // 4. Logic: If present, apply the LLM-defined configuration
    
    // Set Congestion Control (e.g., swap to BBR)
    if (conf->cc[0] != '\0') {
        bpf_setsockopt(skops, SOL_TCP, TCP_CONGESTION, conf->cc, sizeof(conf->cc));
    }

    // Set Window Clamp (ensures large wscale during handshake)
    if (conf->window_clamp > 0) {
        bpf_setsockopt(skops, SOL_TCP, TCP_WINDOW_CLAMP, &conf->window_clamp, sizeof(int));
    }

    // Set Initial Congestion Window (Kernel 7.0 supports this via setsockopt)
    if (conf->init_cwnd > 0) {
        // Note: some kernels require BPF_SOCK_OPS_IWIN_CB for this, 
        // but modern 7.0+ allows direct setsockopt for many TCP params.
        bpf_setsockopt(skops, SOL_TCP, TCP_BPF_IW, &conf->init_cwnd, sizeof(int));
    }

    // Disable Nagle if requested (Low Latency mode)
    if (conf->nodelay > 0) {
        bpf_setsockopt(skops, SOL_TCP, TCP_NODELAY, &conf->nodelay, sizeof(int));
    }

    return 1;
}

char _license[] SEC("license") = "GPL";
```

### Environment Agent

Environment Agent is responsible for generating traffic to the environment.
Receives network-command from environment router, and executes it for first reset() and sends the observation.

After that, it receives action from RL inference engine, and updates the socket configuration in eBPF maps.
Then it re-triggers the network-command, which will be intercepted by eBPF agent and the new configuration will be applied.
Then the new observation will be reported back to environment agent, which then gives back to RL inference engine.

Edge cases handling, after completing episource - this will send signal to environment router, that it is free to execute next one.
And also, if some step takes more than expected time, it will be terminated and reported as error. Later it will be signal router, that it is free.
These scenarios be added to warm pool at router level. 

### Environment Router

Environment router is responsible for routing the network-command to the environment agent.
It maintains a list of environment agents, and assigns the network-command to the environment agent which is free.

This also updates the deployment file of environment agents, to configure number of agents running on EKS cluster.
Based on this and max CPU limit in the karpenter configuration - agents can be scaled to more number of nodes.

Router basically observes the total resets and generally tries to keep 2 warm nodes - so that new requests can be configured.
If there are no available nodes at the moment when reset happens, it will return an error to RL inference engine stating the same.


### RL Inference Engine

Generates reset() and step() calls to the environment.
Connects to environment router, to make all the requests.

Setup will be running on a HuggingFace space.
