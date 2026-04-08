# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.
#
# This source code is licensed under the BSD-style license found in the
# LICENSE file in the root directory of this source tree.

"""
FastAPI application for the Socket Tuner Environment.
"""

try:
    import sys
    import os
    # Add the current directory (server/) to sys.path so gRPC generated 
    # imports like 'import agent_pb2' work correctly within the package
    sys.path.append(os.path.dirname(os.path.abspath(__file__)))
    from openenv.core.env_server.http_server import create_app
    from openenv.core.env_server.gradio_ui import build_gradio_app
except Exception as e:
    raise ImportError(
        "openenv is required for the web interface. Install dependencies with '\n    uv sync\n'"
    ) from e

try:
    from ..models import UISocketTunerAction, SocketTunerObservation
    from .socket_tuner_environment import SocketTunerEnvironment
except Exception:
    from models import UISocketTunerAction, SocketTunerObservation
    from socket_tuner_environment import SocketTunerEnvironment

# --- UI Customization (Local app.py only) ---
import openenv.core.env_server.web_interface as web_interface
import openenv.core.env_server.gradio_ui as gradio_ui
import gradio as gr
import base64
import re
import mimetypes

# Professional labels mapping
# Pydantic field names -> Professional UI Labels
FIELD_TO_LABEL = {
    "cong_algo": "Congestion Control Algorithm",
    "init_cwnd": "Initial Congestion Window (CWND)",
    "no_delay": "Disable Nagle (TCP_NODELAY)",
    "max_pacing_rate": "Max Pacing Rate (bps)",
    "snd_cwnd_clamp": "Send CWND Clamp",
    "window_clamp": "Receive Window Clamp",
    "rto_min": "Min Retransmission Timeout (ms)",
    "retrans_after": "Retransmit After (packets)",
    "enable_ecn": "Enable ECN",
    "pacing_status": "Pacing Control",
    "keepalive_idle": "Keepalive Idle Time (s)"
}

# The internal labels Gradio generates (Title Cased field names)
INTERNAL_LABEL_TO_FIELD = {
    name.replace("_", " ").title(): name for name in FIELD_TO_LABEL
}

# 1. Custom Quick Start behavior
web_interface.get_quick_start_markdown = lambda *args, **kwargs: ""

# 2. Custom README loader
def custom_load_readme(*args, **kwargs):
    server_dir = os.path.dirname(os.path.abspath(__file__))
    readme_path = os.path.join(server_dir, "README.md")
    
    if os.path.exists(readme_path):
        with open(readme_path, "r") as f:
            content = f.read()
        
        # Regex to find local image references e.g. ![alt](path.png)
        # It handles relative paths by checking if they exist in the server directory
        def embed_images(match):
            alt_text = match.group(1)
            img_path = match.group(2)
            
            # Resolve relative path
            full_img_path = os.path.join(server_dir, img_path)
            
            if os.path.exists(full_img_path) and os.path.isfile(full_img_path):
                mime_type, _ = mimetypes.guess_type(full_img_path)
                if not mime_type:
                    mime_type = "image/png"
                
                with open(full_img_path, "rb") as img_file:
                    encoded_string = base64.b64encode(img_file.read()).decode("utf-8")
                    # Wrap the image in an HTML tag to control its width and appearance
                    return (
                        f'<div style="text-align: center; margin: 1.5rem 0;">'
                        f'<img src="data:{mime_type};base64,{encoded_string}" '
                        f'alt="{alt_text}" '
                        f'style="width: 80%; border-radius: 8px; align-items: center;" />'
                        f'</div>'
                    )
            
            return match.group(0) # Return original if not found
            
        # Match markdown images: ![description](path)
        content = re.sub(r'!\[(.*?)\]\((.*?)\)', embed_images, content)
        return content
        
    return "Socket Tuner README"
web_interface._load_readme_from_filesystem = custom_load_readme

# 3. Custom Interface implementation to unify the UI
original_tabbed_interface = gr.TabbedInterface
def custom_tabbed_interface_impl(interface_list, tab_names, *args, **kwargs):
    if len(interface_list) > 1:
        return interface_list[1]
    return original_tabbed_interface(interface_list, tab_names, *args, **kwargs)
gr.TabbedInterface = custom_tabbed_interface_impl

def socket_tuner_ui_builder(
    web_manager,
    action_fields,
    metadata,
    is_chat_env,
    title,
    quick_start_md,
):
    """
    Custom Gradio builder for the Socket Tuner.
    Intercepts component creation to force professional labels.
    """
    # Intercept Gradio components to force our professional labels
    original_textbox = gr.Textbox
    original_number = gr.Number
    original_checkbox = gr.Checkbox
    original_dropdown = gr.Dropdown
    original_accordion = gr.Accordion
    original_column = gr.Column

    def get_prof_label(label):
        if label in INTERNAL_LABEL_TO_FIELD:
            field_name = INTERNAL_LABEL_TO_FIELD[label]
            return FIELD_TO_LABEL.get(field_name, label)
        return label

    def patched_textbox(label=None, **kwargs):
        return original_textbox(label=get_prof_label(label), **kwargs)

    def patched_number(label=None, **kwargs):
        return original_number(label=get_prof_label(label), **kwargs)

    def patched_checkbox(label=None, **kwargs):
        return original_checkbox(label=get_prof_label(label), **kwargs)

    def patched_dropdown(label=None, **kwargs):
        return original_dropdown(label=get_prof_label(label), **kwargs)

    def patched_accordion(label, *a, **kw):
        if label == "README":
            kw["open"] = True
        return original_accordion(label, *a, **kw)

    def patched_column(scale=None, min_width=None, elem_classes=None, **kwargs):
        classes = elem_classes or []
        if "col-left" in classes:
            return original_column(scale=3, min_width=0, elem_classes=classes, **kwargs)
        if "col-right" in classes:
            return original_column(scale=2, min_width=0, elem_classes=classes, **kwargs)
        return original_column(scale=scale, min_width=min_width, elem_classes=elem_classes, **kwargs)

    # Apply patches to the gradio module locally
    import gradio as gr_mod
    gr_mod.Textbox = patched_textbox
    gr_mod.Number = patched_number
    gr_mod.Checkbox = patched_checkbox
    gr_mod.Dropdown = patched_dropdown
    gr_mod.Accordion = patched_accordion
    gr_mod.Column = patched_column

    try:
        demo = build_gradio_app(
            web_manager=web_manager,
            action_fields=action_fields,
            metadata=metadata,
            is_chat_env=is_chat_env,
            title=title,
            quick_start_md=None,
        )
        return demo
    finally:
        # Restore original components
        gr_mod.Textbox = original_textbox
        gr_mod.Number = original_number
        gr_mod.Checkbox = original_checkbox
        gr_mod.Dropdown = original_dropdown
        gr_mod.Accordion = original_accordion
        gr_mod.Column = original_column

# -----------------------------------------------------------

# Create the app with the standard web interface
app = create_app(
    SocketTunerEnvironment,
    UISocketTunerAction,
    SocketTunerObservation,
    env_name="socket_tuner",
    max_concurrent_envs=4,
    gradio_builder=socket_tuner_ui_builder,
)


def main(host: str = "0.0.0.0", port: int = 8000):
    """
    Entry point for direct execution via uv run or python -m.
    """
    import uvicorn
    uvicorn.run(app, host=host, port=port)


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8000)
    args = parser.parse_args()
    main(port=args.port)
