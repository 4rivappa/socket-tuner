# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.
#
# This source code is licensed under the BSD-style license found in the
# LICENSE file in the root directory of this source tree.

"""
Socket Tuner Environment Implementation.

A simple test environment that echoes back messages sent to it.
Perfect for testing HTTP server infrastructure.
"""

import os
import grpc
from uuid import uuid4
import logging

MAX_STEPS = int(os.getenv("MAX_STEPS", "5"))

from openenv.core.env_server.interfaces import Environment
from openenv.core.env_server.types import State, EnvironmentMetadata

try:
    from .agent_pb2 import ResetRequest, StepRequest, CloseRequest
    from .agent_pb2_grpc import EnvAgentStub
except Exception:
    import agent_pb2 as pb
    import agent_pb2_grpc as pb_grpc
    ResetRequest = pb.ResetRequest
    StepRequest = pb.StepRequest
    CloseRequest = pb.CloseRequest
    EnvAgentStub = pb_grpc.EnvAgentStub

try:
    from ..models import SocketTunerAction, SocketTunerObservation, UISocketTunerAction
except Exception:
    from models import SocketTunerAction, SocketTunerObservation, UISocketTunerAction

logger = logging.getLogger(__name__)

### Test: Malicious, Unauthorized network commands
# TASKS = [
#     {
#         "name": "hetzner-throughput",
#         "description": "Optimize throughput for a medium-sized file download (AWS to Hetzner).",
#         "command": "curl -4 -o /dev/null https://ash-speed.hetzner.com/1GB.bin",
#         "difficulty": "Medium"
#     }
# ]

TASKS = [
    {
        "name": "google-latency",
        "description": "Minimize latency for small header-only requests (AWS to GCP/Google Global).",
        "command": "curl -4 -sI -m 5 http://google.com",
        "difficulty": "Easy"
    },
    {
        "name": "hetzner-throughput",
        "description": "Optimize throughput for a medium-sized file download (AWS to Hetzner).",
        "command": "curl -4 -o /dev/null https://ash-speed.hetzner.com/100MB.bin",
        "difficulty": "Medium"
    },
    {
        "name": "github-bandwidth",
        "description": "Maximize bandwidth for a large archive download (AWS to GitHub).",
        "command": "curl -4 -o /dev/null https://github.com/torvalds/linux/archive/refs/tags/v7.0-rc6.tar.gz",
        "difficulty": "Hard"
    }
]

class SocketTunerEnvironment(Environment):
    """
    Real-world environment connecting to EKS-based eBPF agents via Environment Router.
    Supports a curriculum of 3 difficulty levels.
    """

    SUPPORTS_CONCURRENT_SESSIONS: bool = True

    def __init__(self):
        """Initialize the socket_tuner environment with gRPC channel."""
        self._state = State(episode_id=str(uuid4()), step_count=0)
        self._session_id = None
        self._reset_count = 0
        self._baseline_duration = 0
        self._target_ip = ""
        self._target_port = 0
        
        # Get router address from environment variable (e.g. LoadBalancer DNS)
        # NLB ARN: arn:aws:elasticloadbalancing:us-east-1:786221173676:loadbalancer/net/socket-tuner-env-router/d3490d2d14c591e6
        self.router_addr = os.getenv("ENV_ROUTER_ADDR", "socket-tuner-env-router-d3490d2d14c591e6.elb.us-east-1.amazonaws.com:50050")
        
        # Establish persistent channel
        self.channel = grpc.insecure_channel(self.router_addr)
        self.stub = EnvAgentStub(self.channel)
        
        logger.info(f"Connected to Environment Router at {self.router_addr}")

    def reset(self) -> SocketTunerObservation:
        """
        Reset the environment by requesting a new session from the Router.
        Rotates through predefined network tasks.
        """
        # Pick task based on reset count
        task_idx = self._reset_count % len(TASKS)
        current_task = TASKS[task_idx]
        self._reset_count += 1
        
        command = current_task["command"]
        logger.info(f"Resetting for task: {current_task['name']} (Difficulty: {current_task['difficulty']})")
        
        max_retries = 3
        retry_delay = 2 # seconds
        
        for attempt in range(max_retries):
            try:
                response = self.stub.Reset(ResetRequest(command=command))
                if response.success:
                    self._session_id = response.session_id
                    self._state = State(episode_id=self._session_id, step_count=0)
                    
                    obs = response.initial_observation
                    self._baseline_duration = obs.duration_us if obs and obs.duration_us > 0 else 0
                    self._target_ip = obs.remote_ip
                    self._target_port = obs.remote_port
                    
                    if self._baseline_duration == 0:
                        logger.warning(f"Reset attempt {attempt+1}: Baseline duration is 0. Metrics might be unavailable.")
                    
                    break # Success
                else:
                    error_msg = response.message
                    if "busy" in error_msg.lower() or "no free agent" in error_msg.lower():
                        if attempt < max_retries - 1:
                            logger.info(f"Reset attempt {attempt+1}: Cluster busy. Retrying in {retry_delay}s...")
                            import time
                            time.sleep(retry_delay)
                            continue
                    raise RuntimeError(f"Router reset failed: {error_msg}")
            except grpc.RpcError as e:
                if attempt < max_retries - 1:
                    logger.warning(f"Reset attempt {attempt+1} gRPC error: {e}. Retrying...")
                    import time
                    time.sleep(retry_delay)
                    continue
                raise RuntimeError(f"Router connection failed after {max_retries} attempts: {e}")
        else:
            raise RuntimeError(f"Failed to acquire an agent after {max_retries} attempts (Cluster Busy).")
            
        return SocketTunerObservation(
            remote_ip=obs.remote_ip,
            remote_port=obs.remote_port,
            srtt_us=obs.srtt_us,
            mdev_us=obs.mdev_us,
            total_retrans=obs.total_retrans,
            bytes_sent=obs.bytes_sent,
            bytes_received=obs.bytes_received,
            duration_us=obs.duration_us,
            session_id=self._session_id,
            done=False,
            reward=0.01,
            metadata={
                "task_name": current_task["name"],
                "task_description": current_task["description"],
                "difficulty": current_task["difficulty"],
                "step": 0
            }
        )

    def step(self, action: UISocketTunerAction) -> SocketTunerObservation:  # type: ignore[override]
        """
        Apply tuning parameters to the EKS agent and get real metrics back.
        """
        self._state.step_count += 1
        
        try:
            # Map cong_algo string back to int for gRPC
            cong_algo_map = {"cubic": 1, "bbr": 2}
            cong_algo_val = cong_algo_map.get(action.cong_algo, 1)

            req = StepRequest(
                session_id=self._session_id,
                target_ip=self._target_ip,
                target_port=self._target_port,
                max_pacing_rate=action.max_pacing_rate,
                snd_cwnd_clamp=action.snd_cwnd_clamp,
                cong_algo=cong_algo_val,
                init_cwnd=action.init_cwnd,
                window_clamp=action.window_clamp,
                no_delay=action.no_delay,
                rto_min=action.rto_min,
                retrans_after=action.retrans_after,
                enable_ecn=action.enable_ecn,
                pacing_status=action.pacing_status,
                keepalive_idle=action.keepalive_idle
            )
            
            response = self.stub.Step(req)
            obs = response.observation
            
            reward = 0.01
            done_early = False
            reduction = 0.0
            
            if self._baseline_duration > 0 and obs.duration_us > 0:
                # Calculate percentage reduction in duration time
                reduction = (self._baseline_duration - obs.duration_us) / self._baseline_duration
                
                # Any step which reduced time by >=25% gets >=0.5. Max clamped to 0.99.
                if reduction > 0:
                    reward = reduction * 2.0
                
                # Ensure reward is between 0.01 and 0.99 and rounded to 2 decimal places
                reward = round(max(0.01, min(0.99, reward)), 2)
                
                # If it achieves better performance than 50%, we stop early
                if reduction >= 0.5:
                    done_early = True
            
            done = response.done or done_early or self._state.step_count >= MAX_STEPS
            
            if done and self._session_id:
                try:
                    self.stub.Close(CloseRequest(session_id=self._session_id))
                    logger.info(f"Sent explicit Close for session {self._session_id}")
                except Exception as e:
                    logger.warning(f"Failed to send Close request: {e}")

            return SocketTunerObservation(
                remote_ip=obs.remote_ip,
                remote_port=obs.remote_port,
                srtt_us=obs.srtt_us,
                mdev_us=obs.mdev_us,
                total_retrans=obs.total_retrans,
                bytes_sent=obs.bytes_sent,
                bytes_received=obs.bytes_received,
                duration_us=obs.duration_us,
                session_id=self._session_id,
                done=done,
                reward=reward,
                metadata={
                    "step": self._state.step_count,
                    "baseline_duration": self._baseline_duration,
                    "reduction": reduction
                }
            )
        except grpc.RpcError as e:
            logger.error(f"gRPC Step error: {e}")
            raise

    @property
    def state(self) -> State:
        return self._state

    def close(self):
        """Clean up the gRPC channel and release the agent if still active."""
        if self._session_id:
            try:
                # Best-effort close notification
                self.stub.Close(CloseRequest(session_id=self._session_id), timeout=2)
                logger.info(f"Cleanup: Sent Close for session {self._session_id}")
            except Exception:
                pass
            self._session_id = None

        if hasattr(self, 'channel'):
            self.channel.close()

    @property
    def state(self) -> State:
        """
        Get the current environment state.

        Returns:
            Current State with episode_id and step_count
        """
        return self._state

    def get_metadata(self) -> EnvironmentMetadata:
        """Get environment metadata for the web interface."""
        return EnvironmentMetadata(
            name="Socket Tuner",
            description="Real-world EKS-based TCP kernel parameter tuning gym using eBPF agents. Optimize network performance across various tasks.",
            version="1.0.0",
            author="DeepMind Agentic Coding"
        )
