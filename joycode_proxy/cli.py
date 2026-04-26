import logging
import os
import subprocess
import sys
from pathlib import Path

import click
from rich.console import Console

from joycode_proxy.auth import Credentials, load_from_system
from joycode_proxy.ui import (
    VERSION,
    console,
    print_banner,
    print_endpoint_tree,
    print_error,
    print_info,
    print_kv_table,
    print_model_table,
    print_success,
    print_warning,
)

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
    from rich.status import Status
    with Status("[bold cyan]Validating credentials...", console=console):
        client.validate()
    print_success("Credentials validated")
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
    print_banner()
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from joycode_proxy.server import create_app
    app = create_app(client)
    print_endpoint_tree(host, port)
    console.print()
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
    console.print(Panel(f"[dim]{message}[/dim]", title=f"[bold cyan]{model}[/bold cyan]", border_style="cyan"))
    if stream:
        body["stream"] = True
        resp = client.post_stream("/api/saas/openai/v1/chat/completions", body)
        try:
            for line in resp.iter_lines():
                if line:
                    decoded = line.decode("utf-8", errors="replace") if isinstance(line, bytes) else line
                    if decoded.startswith("data: "):
                        import json
                        payload = decoded[6:]
                        if payload == "[DONE]":
                            break
                        try:
                            chunk = json.loads(payload)
                            delta = chunk.get("choices", [{}])[0].get("delta", {})
                            text = delta.get("content", "")
                            if text:
                                console.print(text, end="")
                        except Exception:
                            pass
            console.print()
        finally:
            resp.close()
        return
    from rich.status import Status
    with Status("[bold cyan]Thinking...", console=console):
        resp = client.post("/api/saas/openai/v1/chat/completions", body)
    choices = resp.get("choices", [])
    if choices:
        content = choices[0].get("message", {}).get("content", "")
        console.print()
        console.print(content)
        console.print()


@cli.command()
@click.pass_context
def models(ctx):
    from joycode_proxy.client import DEFAULT_MODEL
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    with Status("[bold cyan]Fetching models...", console=console):
        model_list = client.list_models()
    print_model_table(model_list, DEFAULT_MODEL)


@cli.command()
@click.pass_context
def whoami(ctx):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    with Status("[bold cyan]Fetching user info...", console=console):
        resp = client.user_info()
    data = resp.get("data", {})
    status = "[bold green]Active[/bold green]" if resp.get("code") == 0 else "[bold red]Invalid[/bold red]"
    print_kv_table("User Info", {
        "Name": data.get("realName", "N/A"),
        "ID": data.get("userId", "N/A"),
        "Organization": data.get("orgName", "N/A"),
        "Tenant": data.get("tenant", "N/A"),
        "Status": status,
    })


@cli.command()
def version():
    print_banner()
    print_kv_table("Version Info", {
        "JoyCode Proxy": VERSION,
        "Python": sys.version.split()[0],
    })


@cli.command()
@click.pass_context
def config(ctx):
    print_banner()
    print_kv_table("Configuration", {
        "API Base URL": "https://joycode-api.jd.com",
        "Default Model": "JoyAI-Code",
        "Default Port": "34891",
        "Verbose": str(ctx.obj.get("verbose", False)),
        "Skip Validation": str(ctx.obj.get("skip_validation", False)),
        "Config Dir": str(Path.home() / ".joycode-proxy"),
        "Log Dir": str(Path.home() / ".joycode-proxy" / "logs"),
    })


@cli.command()
@click.option("-H", "--host", default="localhost", help="Proxy host")
@click.option("-p", "--port", default=34891, help="Proxy port")
def check(host: str, port: int):
    import httpx
    from rich.status import Status
    url = f"http://{host}:{port}/health"
    with Status(f"[bold cyan]Checking {url}...", console=console):
        try:
            resp = httpx.get(url, timeout=5.0)
            if resp.status_code == 200:
                print_success(f"Proxy is running at {host}:{port}")
                data = resp.json()
                if "endpoints" in data:
                    for ep in data["endpoints"]:
                        console.print(f"  [green]•[/green] {ep}")
            else:
                print_error(f"Proxy returned status {resp.status_code}")
        except httpx.ConnectError:
            print_error(f"Cannot connect to proxy at {host}:{port}")
        except Exception as exc:
            print_error(str(exc))


@cli.command()
@click.argument("query")
@click.option("-n", "--max-results", default=5, help="Max results")
@click.pass_context
def search(ctx, query: str, max_results: int):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    from rich.table import Table
    from rich import box
    with Status(f'[bold cyan]Searching: "{query}"...', console=console):
        results = client.web_search(query)
    if not results:
        print_warning("No results found.")
        return
    table = Table(title=f"Search Results: {query}", box=box.ROUNDED, show_lines=True)
    table.add_column("#", style="bold", width=3)
    table.add_column("Title", style="cyan")
    table.add_column("URL", style="blue")
    table.add_column("Snippet", max_width=60)
    for i, r in enumerate(results[:max_results], 1):
        title = r.get("title", "N/A")
        url = r.get("url", r.get("link", "N/A"))
        snippet = r.get("snippet", r.get("content", "N/A"))
        if isinstance(snippet, str) and len(snippet) > 120:
            snippet = snippet[:117] + "..."
        table.add_row(str(i), title, url, snippet)
    console.print(table)


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
    print_success("Service installed and started")
    print_kv_table("Service", {
        "Label": "com.joycode.proxy",
        "Plist": str(plist_path),
        "Port": str(port),
        "Logs": str(log_dir) + "/",
    })


@service.command("uninstall")
def service_uninstall():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    subprocess.run(["launchctl", "unload", str(plist_path)], capture_output=True)
    if plist_path.exists():
        plist_path.unlink()
        print_success("Service stopped and removed")
    else:
        print_warning("Service not installed (plist not found)")


@service.command("status")
def service_status():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    if not plist_path.exists():
        print_warning("Service not installed")
        return
    result = subprocess.run(["launchctl", "list"], capture_output=True, text=True)
    found = False
    for line in result.stdout.splitlines():
        if "com.joycode.proxy" in line:
            print_success(f"Service is running")
            console.print(f"  [dim]{line}[/dim]")
            found = True
            break
    if not found:
        print_warning("Service installed but not running")
    console.print(f"\n  Logs: [cyan]{home / '.joycode-proxy' / 'logs'}/[/cyan]")


if __name__ == "__main__":
    cli()
