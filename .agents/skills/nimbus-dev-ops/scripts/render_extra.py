#!/usr/bin/env python3
"""Render CLI 2.34 gaps: env inspection/update and Web suspend/resume."""

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

SERVICE_ID = "srv-dbjl0pe7bikc73fplma0"
SAFE_VALUES = {"NIMBUS_LOG_LEVEL", "NIMBUS_DEPLOY_HANDOFF", "NIMBUS_PUBLIC_HOST", "NIMBUS_ADDR", "PORT"}
DB_KEYS = {"NIMBUS_POSTGRES", "DATABASE_URL", "DATABASE_URL_UNPOOLED"}


class SafeError(Exception):
    """Messages defined locally, containing no provider/user credential data."""


def request(path, method="GET", body=None):
    token = os.environ.get("RENDER_API_KEY")
    if not token:
        raise SafeError("Set RENDER_API_KEY securely in the environment; never pass its value as an argument.")
    req = urllib.request.Request(
        "https://api.render.com/v1/" + path,
        method=method,
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None,
    )
    with urllib.request.urlopen(req, timeout=30) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def fields(value):
    return value.get("envVar", value)


def inspect_env():
    cursor = None
    seen = set()
    result = []
    while True:
        query = {"limit": 100}
        if cursor:
            query["cursor"] = cursor
        _, rows = request(f"services/{SERVICE_ID}/env-vars?" + urllib.parse.urlencode(query))
        for row in rows:
            item = fields(row)
            key, value = item["key"], item.get("value", "")
            output = {"key": key, "configured": bool(value)}
            if key in SAFE_VALUES:
                output["value"] = value
            elif key in DB_KEYS and value:
                try:
                    host = urllib.parse.urlsplit(value).hostname
                    output.update(host=host, pooled="-pooler." in (host or ""))
                except ValueError:
                    output["dsn_format"] = "invalid/non-URL; value hidden"
            result.append(output)
        next_cursor = rows[-1].get("cursor") if rows else None
        if len(rows) < 100 or not next_cursor or next_cursor in seen:
            break
        seen.add(next_cursor)
        cursor = next_cursor
    print(json.dumps({"service_id": SERVICE_ID, "env": result}, ensure_ascii=False, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("env", help="Read names/presence and safe values, hiding credentials")
    edit = sub.add_parser("env-set", help="Update one key from an existing process environment variable")
    edit.add_argument("key")
    edit.add_argument("--from-env", required=True, metavar="VARIABLE_NAME")
    edit.add_argument("--confirm", action="store_true", help="Execute the authorized update")
    for name in ["suspend", "resume"]:
        command = sub.add_parser(name, help=f"{name} this dev Web service")
        command.add_argument("--confirm", action="store_true", help="Execute the authorized action")
    args = parser.parse_args()
    if args.command == "env":
        inspect_env()
        return
    if not args.confirm:
        print(json.dumps({"service_id": SERVICE_ID, "action": args.command, "executed": False,
                          "key": getattr(args, "key", None)}))
        return
    if args.command == "env-set":
        if args.from_env not in os.environ:
            raise SafeError("Source environment variable missing; nothing changed.")
        value = os.environ[args.from_env]
        if args.key in {"NIMBUS_POSTGRES", "DATABASE_URL_UNPOOLED"}:
            try:
                parsed = urllib.parse.urlsplit(value)
                valid = parsed.scheme in {"postgres", "postgresql"} and parsed.hostname
                pooled = "-pooler." in (parsed.hostname or "")
            except ValueError:
                valid, pooled = False, False
            if not valid or pooled:
                raise SafeError("Nimbus requires a PostgreSQL direct URL; nothing changed.")
        path = f"services/{SERVICE_ID}/env-vars/" + urllib.parse.quote(args.key, safe="")
        status, _ = request(path, "PUT", {"value": value})
        _, actual = request(path)
        if fields(actual).get("value") != value:
            raise SafeError("Read-back did not match; inspect state before retrying.")
        print(json.dumps({"service_id": SERVICE_ID, "key": args.key, "http_status": status,
                          "read_back_verified": True, "deployment_required": True}))
    else:
        status, _ = request(f"services/{SERVICE_ID}/{args.command}", "POST")
        print(json.dumps({"service_id": SERVICE_ID, "action": args.command, "http_status": status,
                          "accepted": True, "verify_instances_with_render_cli": True}))


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as error:
        # Provider bodies may echo credentials. Never print them or retry writes.
        print(f"Render API HTTP {error.code}; inspect authorization/state before retrying.", file=sys.stderr)
        sys.exit(1)
    except (urllib.error.URLError, TimeoutError, OSError):
        print("Connection failed/timed out; a write may have applied. Inspect state before retrying.", file=sys.stderr)
        sys.exit(1)
    except SafeError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except ValueError:
        # Includes JSON/URL parse errors: do not print exception data containing secrets.
        print("Invalid input/response or verification failed; no credentials printed. Inspect state before retrying.", file=sys.stderr)
        sys.exit(1)
