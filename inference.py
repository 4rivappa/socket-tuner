"""
Inference Script Example
===================================
MANDATORY
- Before submitting, ensure the following variables are defined in your environment configuration:
    API_BASE_URL   The API endpoint for the LLM.
    MODEL_NAME     The model identifier to use for inference.
    HF_TOKEN       Your Hugging Face / API key.
    LOCAL_IMAGE_NAME The name of the local image to use for the environment if you are using from_docker_image()
                     method

- Defaults are set only for API_BASE_URL and MODEL_NAME 
    (and should reflect your active inference setup):
    API_BASE_URL = os.getenv("API_BASE_URL", "<your-active-endpoint>")
    MODEL_NAME = os.getenv("MODEL_NAME", "<your-active-model>")
    
- The inference script must be named `inference.py` and placed in the root directory of the project
- Participants must use OpenAI Client for all LLM calls using above variables

STDOUT FORMAT
- The script must emit exactly three line types to stdout, in this order:

    [START] task=<task_name> env=<benchmark> model=<model_name>
    [STEP]  step=<n> action=<action_str> reward=<0.00> done=<true|false> error=<msg|null>
    [END]   success=<true|false> steps=<n> score=<score> rewards=<r1,r2,...,rn>

  Rules:
    - One [START] line at episode begin.
    - One [STEP] line per step, immediately after env.step() returns.
    - One [END] line after env.close(), always emitted (even on exception).
    - reward and rewards are formatted to 2 decimal places.
    - done and success are lowercase booleans: true or false.
    - error is the raw last_action_error string, or null if none.
    - All fields on a single line with no newlines within a line.
    - Each tasks should return score in [0, 1]
"""

import json
import os
import textwrap
from typing import List, Optional, Dict

from openai import OpenAI

from client import SocketTunerEnv
from models import LLMSocketTunerAction, LLMSocketTunerObservation

IMAGE_NAME = os.getenv("IMAGE_NAME") # If you are using docker image 
API_KEY = os.getenv("HF_TOKEN") or os.getenv("API_KEY")

API_BASE_URL = os.getenv("API_BASE_URL") or "https://router.huggingface.co/v1"
MODEL_NAME = os.getenv("MODEL_NAME") or "Qwen/Qwen2.5-Coder-7B-Instruct"
# MODEL_NAME = os.getenv("MODEL_NAME") or "meta-llama/Llama-3.1-8B-Instruct"

TASK_NAME = os.getenv("TASK", "socket-tuner")
BENCHMARK = os.getenv("BENCHMARK", "socket-tuning")
MAX_STEPS = 5
TEMPERATURE = 0.7
MAX_TOKENS = 150
SUCCESS_SCORE_THRESHOLD = 0.1  # normalized score in [0, 1]

_MAX_REWARD_PER_STEP = 1.0
MAX_TOTAL_REWARD = MAX_STEPS * _MAX_REWARD_PER_STEP

SYSTEM_PROMPT = textwrap.dedent(
    f"""
    You are an eBPF-based socket tuning agent.
    Goal: Optimize TCP parameters to minimize SRTT and retransmissions.
    
    You must return a JSON object with these exact keys:
    {{
      "max_pacing_rate": 0,
      "snd_cwnd_clamp": 0,
      "cong_algo": "cubic",
      "init_cwnd": 10,
      "window_clamp": 0,
      "no_delay": true,
      "rto_min_ms": 0,
      "retrans_after": 0,
      "enable_ecn": false,
      "pacing_status": false,
      "keepalive_idle_ms": 0
    }}
    
    Tuning Guidelines:
    - cong_algo: "cubic" is generally better for low-latency/low-loss; "bbr" can significantly benefit connections with higher RTT or random packet loss.
    - init_cwnd: Start with 10-20. Use larger jumps (10+) initially. Once you see a positive Reward (>0), use smaller increments (+/- 2) to fine-tune the peak.
    - no_delay: true is highly recommended to reduce latency for small interactive packets.
    - pacing_status: true can help stabilize high-bandwidth transfers (Hard task).
    """
).strip()


def log_start(task: str, env: str, model: str) -> None:
    print(f"[START] task={task} env={env} model={model}", flush=True)


def log_step(step: int, action: str, reward: float, done: bool, error: Optional[str]) -> None:
    error_val = error if error else "null"
    done_val = str(done).lower()
    print(
        f"[STEP] step={step} action={action} reward={reward:.2f} done={done_val} error={error_val}",
        flush=True,
    )


def log_end(success: bool, steps: int, score: float, rewards: List[float]) -> None:
    rewards_str = ",".join(f"{r:.2f}" for r in rewards)
    print(f"[END] success={str(success).lower()} steps={steps} score={score:.3f} rewards={rewards_str}", flush=True)


def build_user_prompt(step: int, last_obs: LLMSocketTunerObservation, last_reward: float, history: List[str]) -> str:
    history_block = "\n".join(history[-4:]) if history else "None"
    
    # providing task details as the environment focuses on time/duration optimization
    task_name = "Network Latency & Throughput Optimization"
    task_desc = "Minimize total connection duration and SRTT by tuning TCP parameters. High rewards are given for significant percentage reductions in duration compared to the baseline."
    difficulty = "Dynamic"
    
    return textwrap.dedent(
        f"""
        Step: {step}
        Current Task: {task_name} ({difficulty} difficulty)
        Task Goal: {task_desc}
        
        Active Target: {last_obs.remote_ip}:{last_obs.remote_port}
        Current SRTT: {last_obs.srtt_ms} ms
        Current Retransmits: {last_obs.total_retrans}
        Last Reward: {last_reward:.2f}
        
        Previous steps:
        {history_block}
        
        Provide the next tuning JSON. Optimize for the specific task goal.
        """
    ).strip()


def get_model_action(client: OpenAI, step: int, last_obs: LLMSocketTunerObservation, last_reward: float, history: List[str]) -> LLMSocketTunerAction:
    user_prompt = build_user_prompt(step, last_obs, last_reward, history)
    print(f"\n[DEBUG] --- LLM Prompt (Step {step}) ---\n{user_prompt}\n[DEBUG] --------------------------", flush=True)
    try:
        completion = client.chat.completions.create(
            model=MODEL_NAME,
            messages=[
                {"role": "system", "content": SYSTEM_PROMPT},
                {"role": "user", "content": user_prompt},
            ],
            temperature=TEMPERATURE,
            max_tokens=MAX_TOKENS,
            response_format={"type": "json_object"},
        )
        text = (completion.choices[0].message.content or "").strip()
        print(f"[DEBUG] Raw LLM Response: {text}", flush=True)
        data = json.loads(text)
        action = LLMSocketTunerAction(**data)
        
        # Use the UI action which excludes target IP/port (managed by the environment server)
        return action.to_ui_action()
    except Exception as exc:
        print(f"[DEBUG] Model request failed or parsing failed: {exc}", flush=True)
        # Fallback config
        fallback = LLMSocketTunerAction(
            cong_algo="cubic",
            init_cwnd=10,
            no_delay=False
        )
        return fallback.to_ui_action()


def main() -> None:
    client = OpenAI(base_url=API_BASE_URL, api_key=API_KEY)

    env_base = SocketTunerEnv.from_docker_image(IMAGE_NAME) if IMAGE_NAME else SocketTunerEnv(base_url="http://localhost:8000")
    env = env_base.sync()

    all_rewards = []
    num_tasks = 3
    success = False
    score = 0.0
    
    log_start(task=TASK_NAME, env=BENCHMARK, model=MODEL_NAME)

    try:
        for episode in range(1, num_tasks + 1):
            episode_history: List[Dict] = []
            steps_taken = 0
            
            with env:
                # Retry reset if agent is busy
                max_retries = 3
                result = None
                for attempt in range(max_retries):
                    try:
                        result = env.reset()
                        break
                    except Exception as e:
                        if "no free agents" in str(e).lower() and attempt < max_retries - 1:
                            print(f"[DEBUG] Agents busy, retrying in 5s... (Attempt {attempt+1}/{max_retries})")
                            import time
                            time.sleep(5)
                        else:
                            raise e
                
                if not result:
                    continue
                last_obs = result.observation
                
                # Robustness: If the initial observation is empty, try one more reset to get a valid baseline target
                if not last_obs.remote_ip:
                    print("[DEBUG] Initial observation empty, re-resetting to find target...", flush=True)
                    import time
                    time.sleep(2)
                    result = env.reset()
                    last_obs = result.observation

                baseline_metrics = {
                    "srtt_us": last_obs.srtt_ms * 1000.0,
                    "total_retrans": last_obs.total_retrans
                }
                
                task_info = {
                    "task_name": last_obs.metadata.get("task_name", "unknown"),
                    "task_description": last_obs.metadata.get("task_description", "No description"),
                    "difficulty": last_obs.metadata.get("difficulty", "Unknown")
                }
                print(f"\n--- Starting Episode {episode}/{num_tasks}: {task_info['task_name']} ---")

                for step in range(1, MAX_STEPS + 1):
                    if result.done:
                        break

                    # For the history, we need a history of strings for the user prompt
                    history_for_prompt = [f"Step {h['step']}: {h['action']} -> {h['reward']:.2f}" for h in episode_history]
                    
                    action = get_model_action(client, step, last_obs, 0.0 if not episode_history else episode_history[-1]["reward"], history_for_prompt)
                    action_str = f"algo={action.cong_algo},init_cwnd={action.init_cwnd}"

                    # Robustness: Retry the step if we hit transient environment errors
                    max_step_retries = 3
                    for attempt in range(max_step_retries):
                        try:
                            result = env.step(action)
                            break
                        except Exception as e:
                            err_msg = str(e).lower()
                            is_transient = "no metric found" in err_msg or "invalid ipv4" in err_msg
                            if is_transient and attempt < max_step_retries - 1:
                                print(f"[DEBUG] Step failed (transient): {e}. Retrying {attempt+1}/{max_step_retries}...", flush=True)
                                import time
                                time.sleep(2)
                                continue
                            raise e

                    last_obs = result.observation

                    # Basic online reward
                    online_reward = result.reward or 0.0
                    done = result.done
                    error = None

                    episode_history.append({
                        "step": step,
                        "action": action_str,
                        "obs": last_obs,
                        "reward": online_reward
                    })
                    
                    steps_taken = step
                    log_step(step=step, action=action_str, reward=online_reward, done=done, error=error)

                    if done:
                        break

            # --- After Episode: Using Online Rewards ---
            episode_rewards = [h["reward"] for h in episode_history]
            all_rewards.extend(episode_rewards)
            avg_reward = sum(episode_rewards)/len(episode_rewards) if episode_rewards else 0
            print(f"--- Episode Review: Avg Reward: {avg_reward:.2f} ---")

        total_max_reward = num_tasks * MAX_TOTAL_REWARD
        score = sum(all_rewards) / total_max_reward if total_max_reward > 0 else 0.0
        score = min(max(score, 0.0), 1.0)
        success = score >= SUCCESS_SCORE_THRESHOLD

    finally:
        try:
            env.close()
        except Exception as e:
            print(f"[DEBUG] env.close() error: {e}", flush=True)
        log_end(success=success, steps=len(all_rewards), score=score, rewards=all_rewards)


if __name__ == "__main__":
    main()
