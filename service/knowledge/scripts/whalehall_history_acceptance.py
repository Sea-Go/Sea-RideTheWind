"""One isolated real RTW/UserCenter -> WhaleHall Bun history handoff."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import time


ROOT = Path(__file__).resolve().parents[3]


def wait_ready(path: Path, process: subprocess.Popen, timeout: float) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if path.is_file():
            if path.stat().st_mode & 0o777 != 0o600:
                raise RuntimeError("RTW handoff ready file is not private")
            return
        if process.poll() is not None:
            raise RuntimeError("RTW shared history source exited before ready")
        time.sleep(0.1)
    raise RuntimeError("RTW shared history source timed out before ready")


def run_bun(whalehall: Path, ready: Path, log: Path) -> dict:
    env = os.environ.copy()
    env["RTW_CLOUD_HISTORY_READY"] = str(ready)
    result = subprocess.run(["bun", "tests/rtw-cloud-history-handoff.ts"], cwd=whalehall,
                            env=env, capture_output=True, timeout=75)
    log.write_bytes(result.stdout + result.stderr)
    log.chmod(0o600)
    if result.returncode:
        raise RuntimeError(f"WhaleHall Bun history gate failed; inspect {log}")
    try:
        lines = [line for line in result.stdout.decode().splitlines() if line.startswith("{")]
        return json.loads(lines[-1])
    except (UnicodeError, ValueError, IndexError) as error:
        raise RuntimeError("WhaleHall Bun gate did not return a safe result") from error


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--whalehall-root", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    whalehall, output = Path(args.whalehall_root).resolve(), Path(args.output).resolve()
    if not (whalehall / "tests" / "rtw-cloud-history-handoff.ts").is_file():
        raise RuntimeError("WhaleHall handoff test unavailable")
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    paths = {name: output / name for name in ("available.json", "available.release", "withdrawn.json", "withdrawn.release")}
    env = os.environ.copy()
    env.update(KNOWLEDGE_SHARED_HISTORY_READY=str(paths["available.json"]),
               KNOWLEDGE_SHARED_HISTORY_RELEASE=str(paths["available.release"]),
               KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_READY=str(paths["withdrawn.json"]),
               KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_RELEASE=str(paths["withdrawn.release"]))
    source_log = output / "rtw-source.log"
    with source_log.open("wb") as sink:
        source = subprocess.Popen(["bash", "service/knowledge/scripts/history_shared_acceptance.sh"],
                                  cwd=ROOT, env=env, stdout=sink, stderr=subprocess.STDOUT)
        try:
            wait_ready(paths["available.json"], source, 180)
            available = run_bun(whalehall, paths["available.json"], output / "whalehall-available.log")
            paths["available.release"].touch(exist_ok=False)
            wait_ready(paths["withdrawn.json"], source, 180)
            withdrawn = run_bun(whalehall, paths["withdrawn.json"], output / "whalehall-withdrawn.log")
            paths["withdrawn.release"].touch(exist_ok=False)
            if source.wait(timeout=120) != 0:
                raise RuntimeError(f"RTW shared source failed after release; inspect {source_log}")
            if available.get("stage") != "available" or withdrawn.get("stage") != "withdrawn" or \
                    available.get("otherAccountItems") != 0 or withdrawn.get("otherAccountItems") != 0:
                raise RuntimeError("RTW/WhaleHall stage or identity contract differs")
            report = {"status": "passed", "source": "isolated_real_RTW_UserCenter_Knowledge_HTTP",
                      "client": "WhaleHall_Bun_RTWCloudHistoryClient", "stages": [available, withdrawn],
                      "renderer_window_visual": "not_verified", "production_account_binding": "not_connected"}
            (output / "report.json").write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
            print(f"Final report: {output / 'report.json'}")
        finally:
            for name in ("available.release", "withdrawn.release"):
                paths[name].touch(exist_ok=True)
            if source.poll() is None:
                try:
                    source.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    source.terminate()
                    source.wait(timeout=10)


if __name__ == "__main__":
    main()
