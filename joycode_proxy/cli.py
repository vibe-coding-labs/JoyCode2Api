import logging
import os
import subprocess
import sys
from pathlib import Path

import click

from joycode_proxy.auth import Credentials, load_from_system

log = logging.getLogger("joycode-proxy")


def _resolve_client(ptkey: str, userid: str, skip_validation: bool = False):
    from joycode_proxy.client import Client
    if ptkey and userid:
        creds = Credentials(pt_key=ptkey, user_id=userid)
        source = "flags"
    else:
        creds = load_from_system()
        source = "auto-detected"
        if ptkey:
            creds.pt_key = ptkey
            source = "flags+auto-detected"
        if userid:
            creds.user_id = userid
            source = "flags+auto-detected"
    log.info("Credentials source: %s (userId=%s)", source, creds.user_id)
    client = Client(creds.pt_key, creds.user_id)
    if skip_validation:
        log.info("Credential validation skipped (--skip-validation)")
        return client
    log.info("Validating credentials...")
    client.validate()
    log.info("Credentials validated successfully")
    return client


@click.group()
@click.option("-k", "--ptkey", default="", help="JoyCode ptKey (auto-detected if empty)")
@click.option("-u", "--userid", default="", help="JoyCode userID (auto-detected if empty)")
@click.option("--skip-validation", is_flag=True, help="Skip credential validation")
@click.option("-v", "--verbose", is_flag=True, help="Enable debug logging")
@click.pass_context
def cli(ctx, ptkey: str, userid: str, skip_validation: bool, verbose: bool):
    ctx.ensure_object(dict)
    ctx.obj["ptkey"] = ptkey
    ctx.obj["userid"] = userid
    ctx.obj["skip_validation"] = skip_validation
    ctx.obj["verbose"] = verbose
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(level=level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")


@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from joycode_proxy.server import create_app
    app = create_app(client)
    click.echo("  Endpoints:")
    click.echo("    POST /v1/chat/completions  - Chat (OpenAI format)")
    click.echo("    POST /v1/messages          - Chat (Anthropic/Claude Code format)")
    click.echo("    POST /v1/web-search        - Web Search")
    click.echo("    POST /v1/rerank            - Rerank documents")
    click.echo("    GET  /v1/models            - Model list")
    click.echo("    GET  /health               - Health check")
    click.echo()
    click.echo("  Claude Code setup:")
    click.echo(f"    export ANTHROPIC_BASE_URL=http://{host}:{port}")
    click.echo("    export ANTHROPIC_API_KEY=joycode")
    log_level = "debug" if ctx.obj.get("verbose") else "info"
    uvicorn.run(app, host=host, port=port, log_level=log_level)


@cli.command()
@click.argument("message")
@click.option("-m", "--model", default="JoyAI-Code", help="Model name")
@click.option("-s", "--stream", is_flag=True, help="Stream output")
@click.option("--max-tokens", default=64000, help="Max output tokens")
@click.pass_context
def chat(ctx, message: str, model: str, stream: bool, max_tokens: int):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    body = {
        "model": model,
        "messages": [{"role": "user", "content": message}],
        "stream": False,
        "max_tokens": max_tokens,
    }
    if stream:
        body["stream"] = True
        resp = client.post_stream("/api/saas/openai/v1/chat/completions", body)
        try:
            for line in resp.iter_lines():
                if line:
                    click.echo(line)
        finally:
            resp.close()
        return
    resp = client.post("/api/saas/openai/v1/chat/completions", body)
    choices = resp.get("choices", [])
    if choices:
        click.echo(choices[0].get("message", {}).get("content", ""))


@cli.command()
@click.pass_context
def models(ctx):
    from joycode_proxy.client import DEFAULT_MODEL
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    model_list = client.list_models()
    for m in model_list:
        label = m.get("label", "")
        api_model = m.get("chatApiModel", "")
        ctx_max = m.get("maxTotalTokens", 0)
        out_max = m.get("respMaxTokens", 0)
        pref = " *" if api_model == DEFAULT_MODEL else ""
        click.echo(f"  {label} ({api_model}) ctx={ctx_max} out={out_max}{pref}")


@cli.command()
@click.pass_context
def whoami(ctx):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    resp = client.user_info()
    data = resp.get("data", {})
    click.echo(f"  用户: {data.get('realName', 'N/A')}")
    click.echo(f"  ID: {data.get('userId', 'N/A')}")
    click.echo(f"  组织: {data.get('orgName', 'N/A')}")
    click.echo(f"  租户: {data.get('tenant', 'N/A')}")
    status = "有效" if resp.get("code") == 0 else "无效"
    click.echo(f"  状态: {status}")


@cli.group()
def service():
    pass


@service.command("install")
@click.option("-p", "--port", default=34891, help="Bind port")
def service_install(port: int):
    bin_path = Path(sys.executable).resolve()
    home = Path.home()
    log_dir = home / ".joycode-proxy" / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    plist = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.joycode.proxy</string>
    <key>ProgramArguments</key>
    <array>
        <string>{bin_path}</string>
        <string>-m</string>
        <string>joycode_proxy.cli</string>
        <string>serve</string>
        <string>--port</string>
        <string>{port}</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>{log_dir / "stdout.log"}</string>
    <key>StandardErrorPath</key><string>{log_dir / "stderr.log"}</string>
    <key>EnvironmentVariables</key><dict><key>HOME</key><string>{home}</string></dict>
</dict>
</plist>"""
    plist_path.write_text(plist)
    subprocess.run(["launchctl", "load", str(plist_path)], check=True)
    click.echo("Service installed and started.")
    click.echo(f"  Label:   com.joycode.proxy")
    click.echo(f"  Plist:   {plist_path}")
    click.echo(f"  Port:    {port}")
    click.echo(f"  Logs:    {log_dir}/")


@service.command("uninstall")
def service_uninstall():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    subprocess.run(["launchctl", "unload", str(plist_path)], capture_output=True)
    if plist_path.exists():
        plist_path.unlink()
        click.echo("Service stopped and removed.")
    else:
        click.echo("Service not installed (plist not found).")


@service.command("status")
def service_status():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    if not plist_path.exists():
        click.echo("Service not installed.")
        return
    result = subprocess.run(["launchctl", "list"], capture_output=True, text=True)
    found = False
    for line in result.stdout.splitlines():
        if "com.joycode.proxy" in line:
            click.echo(f"Service status: {line}")
            found = True
            break
    if not found:
        click.echo("Service installed but not running.")
    click.echo(f"\nLogs: {home / '.joycode-proxy' / 'logs'}/")


if __name__ == "__main__":
    cli()
