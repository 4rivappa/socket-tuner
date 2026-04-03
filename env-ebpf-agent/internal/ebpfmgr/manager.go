package ebpfmgr

import (
	"fmt"

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

// AttachCgroup attaches both the connect4 and sockops programs to a given cgroup path
func (m *Manager) AttachCgroup(cgroupPath string) error {
	// Attach connect4 — captures PID at connect() syscall time
	lConn, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupPath,
		Attach:  ciliumebpf.AttachCGroupInet4Connect,
		Program: m.objs.BpfConnect4,
	})
	if err != nil {
		return fmt.Errorf("attaching connect4 to cgroup: %w", err)
	}
	m.links = append(m.links, lConn)

	// Attach sockops — applies tuning and captures metrics
	lSockops, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupPath,
		Attach:  ciliumebpf.AttachCGroupSockOps,
		Program: m.objs.BpfSockmap,
	})
	if err != nil {
		return fmt.Errorf("attaching sockops to cgroup: %w", err)
	}
	m.links = append(m.links, lSockops)

	return nil
}

func (m *Manager) Close() {
	for _, l := range m.links {
		l.Close()
	}
	m.objs.Close()
}

// SetAction configures the eBPF action map keyed by PID
func (m *Manager) SetAction(pid uint32, maxPacing uint32, sndCwndClamp uint32, congAlgo string, initCwnd uint32, windowClamp uint32, noDelay bool) error {
	var algo uint32
	switch congAlgo {
	case "cubic":
		algo = 1
	case "bbr":
		algo = 2
	case "reno":
		algo = 3
	}

	var nd uint32
	if noDelay {
		nd = 1
	}

	val := ebpf.BpfTuningAction{
		MaxPacingRate: maxPacing,
		SndCwndClamp:  sndCwndClamp,
		CongAlgo:      algo,
		InitCwnd:      initCwnd,
		WindowClamp:   windowClamp,
		NoDelay:       nd,
	}

	return m.objs.ActionMap.Put(pid, val)
}

// GetMetrics retrieves the final metrics stored by the eBPF program, keyed by PID
func (m *Manager) GetMetrics(pid uint32) (*ebpf.BpfTuningMetrics, error) {
	var metrics ebpf.BpfTuningMetrics
	if err := m.objs.MetricsMap.Lookup(&pid, &metrics); err != nil {
		return nil, fmt.Errorf("lookup metrics for PID %d: %w", pid, err)
	}

	return &metrics, nil
}

// CleanupPID removes both action and metrics entries for a finished PID
func (m *Manager) CleanupPID(pid uint32) {
	m.objs.ActionMap.Delete(&pid)
	m.objs.MetricsMap.Delete(&pid)
}
