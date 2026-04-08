# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.
#
# This source code is licensed under the BSD-style license found in the
# LICENSE file in the root directory of this source tree.

"""Socket Tuner Environment Client."""

from typing import Dict

from openenv.core import EnvClient
from openenv.core.client_types import StepResult
from openenv.core.env_server.types import State

try:
    from .models import (
        SocketTunerAction,
        SocketTunerObservation,
        LLMSocketTunerAction,
        LLMSocketTunerObservation
    )
except ImportError:
    from models import (
        SocketTunerAction,
        SocketTunerObservation,
        LLMSocketTunerAction,
        LLMSocketTunerObservation
    )


class SocketTunerEnv(
    EnvClient[SocketTunerAction, LLMSocketTunerObservation, State]
):
    """
    Client for the Socket Tuner Environment.

    This client maintains a persistent WebSocket connection to the environment server,
    enabling efficient multi-step interactions with lower latency.
    Each client instance has its own dedicated environment session on the server.

    Example:
        >>> # Connect to a running server
        >>> with SocketTunerEnv(base_url="http://localhost:8000") as client:
        ...     result = client.reset()
        ...     print(result.observation.remote_ip, result.observation.srtt_ms)
        ...
        ...     result = client.step(LLMSocketTunerAction(target_ip="1.2.3.4", target_port=80, cong_algo="bbr", init_cwnd=20, no_delay=True))
        ...     print(result.observation.srtt_ms)

    Example with Docker:
        >>> # Automatically start container and connect
        >>> client = SocketTunerEnv.from_docker_image("socket_tuner-env:latest")
        >>> try:
        ...     result = client.reset()
        ...     result = client.step(LLMSocketTunerAction(target_ip="1.2.3.4", target_port=80, cong_algo="cubic", init_cwnd=10, no_delay=False))
        ... finally:
        ...     client.close()
    """

    def _step_payload(self, action: SocketTunerAction) -> Dict:
        """
        Convert SocketTunerAction to JSON payload for step message.
        """
        return action.model_dump()

    def _parse_result(self, payload: Dict) -> StepResult[LLMSocketTunerObservation]:
        """
        Parse server response into StepResult[LLMSocketTunerObservation].
        """
        obs_data = payload.get("observation", {})
        
        # Merge reward and done status into the observation
        reward = payload.get("reward", 0.0)
        done = payload.get("done", False)
        
        observation = SocketTunerObservation(
            **obs_data,
            reward=reward,
            done=done
        )
        
        # Wrap the low-level observation into the LLM-friendly observation
        llm_observation = LLMSocketTunerObservation.wrap(observation)

        return StepResult(
            observation=llm_observation,
            reward=reward,
            done=done,
        )

    def _parse_state(self, payload: Dict) -> State:
        """
        Parse server response into State object.

        Args:
            payload: JSON response from state request

        Returns:
            State object with episode_id and step_count
        """
        return State(
            episode_id=payload.get("episode_id"),
            step_count=payload.get("step_count", 0),
        )
