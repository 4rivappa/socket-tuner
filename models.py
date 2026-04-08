# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.
#
# This source code is licensed under the BSD-style license found in the
# LICENSE file in the root directory of this source tree.

"""
Data models for the Socket Tuner Environment.

The socket_tuner environment simulates TCP socket parameter tuning via eBPF properties.
"""

from typing import Dict, Literal, Optional
from openenv.core.env_server.types import Action, Observation
from pydantic import BaseModel, Field


class SocketTunerAction(Action):
    """Action for the Socket Tuner environment - eBPF TCP socket tuning parameters."""

    target_ip: str = Field(..., description="Target Destination IP")
    target_port: int = Field(..., description="Target Destination Port")
    
    max_pacing_rate: int = Field(default=0, description="Pacing rate in bytes per second")
    snd_cwnd_clamp: int = Field(default=0, description="Congestion window clamp")
    cong_algo: int = Field(default=1, description="Congestion algorithm (enum/index)")
    init_cwnd: int = Field(default=10, description="Initial congestion window")
    window_clamp: int = Field(default=0, description="Max advertised window")
    no_delay: int = Field(default=0, description="1 to disable Nagle's algorithm")
    rto_min: int = Field(default=0, description="Minimum RTO in ms")
    retrans_after: int = Field(default=0, description="Retransmit after N packets")
    enable_ecn: int = Field(default=0, description="1 to enable ECN")
    pacing_status: int = Field(default=0, description="Pacing enable/disable")
    keepalive_idle: int = Field(default=0, description="TCP keepalive idle time")


class UISocketTunerAction(Action):
    """UI-friendly action that excludes network targets (managed by environment)."""
    
    max_pacing_rate: int = Field(default=0, description="Pacing rate in bytes per second")
    snd_cwnd_clamp: int = Field(default=0, description="Congestion window clamp")
    cong_algo: Literal["cubic", "bbr"] = Field(default="cubic", description="TCP Congestion Control Algorithm")
    init_cwnd: int = Field(default=10, description="Initial congestion window")
    window_clamp: int = Field(default=0, description="Max advertised window")
    no_delay: int = Field(default=0, description="1 to disable Nagle's algorithm")
    rto_min: int = Field(default=0, description="Minimum RTO in ms")
    retrans_after: int = Field(default=0, description="Retransmit after N packets")
    enable_ecn: int = Field(default=0, description="1 to enable ECN")
    pacing_status: int = Field(default=0, description="Pacing enable/disable")
    keepalive_idle: int = Field(default=0, description="TCP keepalive idle time")


class SocketTunerObservation(Observation):
    """Observation from the Socket Tuner environment - network metrics collected via eBPF."""

    remote_ip: str = Field(default="", description="Remote Destination IP")
    remote_port: int = Field(default=0, description="Remote Destination Port")
    srtt_us: int = Field(default=0, description="Final smoothed RTT in us")
    mdev_us: int = Field(default=0, description="RTT variance in us")
    total_retrans: int = Field(default=0, description="Total retransmissions")
    bytes_sent: int = Field(default=0, description="Bytes sent during connection")
    bytes_received: int = Field(default=0, description="Bytes received during connection")
    duration_us: int = Field(default=0, description="Connection duration in us")
    
    session_id: str = Field(default="", description="Active session ID from router")


class LLMSocketTunerAction(Action):
    """LLM-friendly action wrapper for Socket Tuner environment."""
    
    max_pacing_rate: int = Field(default=0, description="Pacing rate in bytes per second")
    snd_cwnd_clamp: int = Field(default=0, description="Congestion window clamp")
    cong_algo: Literal["cubic", "bbr"] = Field(default="cubic", description="Congestion algorithm")
    init_cwnd: int = Field(default=10, description="Initial congestion window")
    window_clamp: int = Field(default=0, description="Max advertised window")
    no_delay: bool = Field(default=False, description="Disable Nagle's algorithm")
    rto_min_ms: int = Field(default=0, description="Minimum RTO in ms")
    retrans_after: int = Field(default=0, description="Retransmit after N packets")
    enable_ecn: bool = Field(default=False, description="Enable ECN")
    pacing_status: bool = Field(default=False, description="Pacing enable/disable")
    keepalive_idle_ms: int = Field(default=0, description="TCP keepalive idle time in ms")

    def unwrap(self, target_ip: str, target_port: int) -> SocketTunerAction:
        """Convert this LLM-friendly action to the low-level eBPF action."""
        return SocketTunerAction(
            target_ip=target_ip,
            target_port=target_port,
            max_pacing_rate=self.max_pacing_rate,
            snd_cwnd_clamp=self.snd_cwnd_clamp,
            cong_algo=1 if self.cong_algo == "cubic" else 2,
            init_cwnd=self.init_cwnd,
            window_clamp=self.window_clamp,
            no_delay=1 if self.no_delay else 0,
            rto_min=self.rto_min_ms,
            retrans_after=self.retrans_after,
            enable_ecn=1 if self.enable_ecn else 0,
            pacing_status=1 if self.pacing_status else 0,
            keepalive_idle=self.keepalive_idle_ms,
        )

    def to_ui_action(self) -> UISocketTunerAction:
        """Convert this LLM-friendly action to the UI-friendly action (no target IP/Port)."""
        return UISocketTunerAction(
            max_pacing_rate=self.max_pacing_rate,
            snd_cwnd_clamp=self.snd_cwnd_clamp,
            cong_algo=self.cong_algo,
            init_cwnd=self.init_cwnd,
            window_clamp=self.window_clamp,
            no_delay=1 if self.no_delay else 0,
            rto_min=self.rto_min_ms,
            retrans_after=self.retrans_after,
            enable_ecn=1 if self.enable_ecn else 0,
            pacing_status=1 if self.pacing_status else 0,
            keepalive_idle=self.keepalive_idle_ms,
        )

    @classmethod
    def wrap(cls, action: SocketTunerAction) -> "LLMSocketTunerAction":
        """Convert a low-level eBPF action to this LLM-friendly action."""
        return cls(
            max_pacing_rate=action.max_pacing_rate,
            snd_cwnd_clamp=action.snd_cwnd_clamp,
            cong_algo="cubic" if action.cong_algo == 1 else "bbr",
            init_cwnd=action.init_cwnd,
            window_clamp=action.window_clamp,
            no_delay=bool(action.no_delay),
            rto_min_ms=action.rto_min,
            retrans_after=action.retrans_after,
            enable_ecn=bool(action.enable_ecn),
            pacing_status=bool(action.pacing_status),
            keepalive_idle_ms=action.keepalive_idle,
        )


class LLMSocketTunerObservation(Observation):
    """LLM-friendly observation wrapper for Socket Tuner environment."""
    
    remote_ip: str = Field(default="", description="Remote Destination IP")
    remote_port: int = Field(default=0, description="Remote Destination Port")
    srtt_ms: float = Field(default=0.0, description="Final smoothed RTT in ms")
    mdev_ms: float = Field(default=0.0, description="RTT variance in ms")
    total_retrans: int = Field(default=0, description="Total retransmissions")
    bytes_sent: int = Field(default=0, description="Bytes sent during connection")
    bytes_received: int = Field(default=0, description="Bytes received during connection")
    total_duration_ms: float = Field(default=0.0, description="Connection duration in ms")
    
    session_id: str = Field(default="", description="Active session ID from router")

    @classmethod
    def wrap(cls, obs: SocketTunerObservation) -> "LLMSocketTunerObservation":
        """Convert a low-level eBPF observation to this LLM-friendly observation."""
        return cls(
            remote_ip=obs.remote_ip,
            remote_port=obs.remote_port,
            srtt_ms=obs.srtt_us / 1000.0,
            mdev_ms=obs.mdev_us / 1000.0,
            total_retrans=obs.total_retrans,
            bytes_sent=obs.bytes_sent,
            bytes_received=obs.bytes_received,
            total_duration_ms=obs.duration_us / 1000.0,
            session_id=obs.session_id,
            reward=getattr(obs, 'reward', 0.0),
            done=getattr(obs, 'done', False),
            metadata=getattr(obs, 'metadata', {}),
        )
