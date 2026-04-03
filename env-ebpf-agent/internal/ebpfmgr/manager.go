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
func (m *Manager) SetAction(ip string, port uint32, maxPacing uint32, cwndClamp uint32) error {
	key, err := ipToKey(ip, port)
	if err != nil {
		return err
	}

	val := ebpf.BpfTuningAction{
		MaxPacingRate: maxPacing,
		SndCwndClamp:  cwndClamp,
	}

	return m.objs.ActionMap.Put(key, val)
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
