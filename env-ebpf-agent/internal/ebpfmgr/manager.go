package ebpfmgr

import (
	"encoding/binary"
	"fmt"
	"net"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

	"socket-tuner/env-ebpf-agent/ebpf"
)

type Manager struct {
	objs  ebpf.BpfObjects
	links []link.Link
}

func NewManager() (*Manager, error) {
	var objs ebpf.BpfObjects
	if err := ebpf.LoadBpfObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading objects: %w", err)
	}

	return &Manager{
		objs: objs,
	}, nil
}

// AttachCgroup attaches the sockops program to a given cgroup path
func (m *Manager) AttachCgroup(cgroupPath string) error {
	// Usually cgroupV2 path like /sys/fs/cgroup
	l, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupPath,
		Attach:  ciliumebpf.AttachCGroupSockOps,
		Program: m.objs.BpfSockmap,
	})
	
	if err != nil {
		return fmt.Errorf("attaching to cgroup: %w", err)
	}
	
	m.links = append(m.links, l)
	return nil
}

func (m *Manager) Close() {
	for _, l := range m.links {
		l.Close()
	}
	m.objs.Close()
}

func ipToKey(ipStr string, port uint32) (ebpf.BpfIpPortKey, error) {
	parsedIP := net.ParseIP(ipStr).To4()
	if parsedIP == nil {
		return ebpf.BpfIpPortKey{}, fmt.Errorf("invalid IPv4 address")
	}
	
	var key ebpf.BpfIpPortKey
	// Use LittleEndian usually for host endian mapping of raw bytes on x86
	key.Ip = binary.LittleEndian.Uint32(parsedIP)
	key.Port = 0 // Ignore port for now to avoid network byte order issues
	
	return key, nil
}

// SetAction configures the eBPF map with the RL action parameters
func (m *Manager) SetAction(ip string, port uint32, action *ebpf.BpfTuningAction) error {
	key, err := ipToKey(ip, port)
	if err != nil {
		return err
	}

	return m.objs.ActionMap.Put(key, action)
}

// GetMetrics retrieves the final metrics stored by the eBPF program
func (m *Manager) GetMetrics(ip string, port uint32) (*ebpf.BpfTuningMetrics, error) {
	key, err := ipToKey(ip, port)
	if err != nil {
		return nil, err
	}

	var metrics ebpf.BpfTuningMetrics
	if err := m.objs.MetricsMap.Lookup(&key, &metrics); err != nil {
		return nil, err
	}

	return &metrics, nil
}

// GetMetricForPid looks up exact metric payload for a specific process ID
func (m *Manager) GetMetricForPid(pid uint32) (*ebpf.BpfTuningMetrics, string, uint32, error) {
	var metric ebpf.BpfTuningMetrics
	if err := m.objs.MetricsMap.Lookup(&pid, &metric); err != nil {
		return nil, "", 0, fmt.Errorf("no metric found for PID %d: %w", pid, err)
	}

	ipBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(ipBytes, metric.RemoteIp)
	ipStr := net.IP(ipBytes).String()

	return &metric, ipStr, metric.RemotePort, nil
}
