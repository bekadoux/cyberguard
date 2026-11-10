#!/usr/bin/env python3
"""One explicit development-only demo incident; not a general CSV importer.

Never run against production. Without --apply this prints the selected fixture.
Stores the created incident ID under .local so repeated calls are idempotent
on this developer checkout unless --force is explicitly supplied.
"""
import argparse
import csv
import json
from http.cookiejar import CookieJar
from pathlib import Path
from urllib.request import HTTPCookieProcessor, Request, build_opener

ROOT = Path(__file__).resolve().parents[1]
MARKER = ROOT / ".local" / "demo-handoff-incident.json"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api", default="http://127.0.0.1:8088/api")
    parser.add_argument("--user", default="employee_a")
    parser.add_argument("--password-file", type=Path)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--force", action="store_true", help="Explicitly create another demo incident")
    args = parser.parse_args()
    if args.api not in ("http://127.0.0.1:8088/api", "http://127.0.0.1:8080"):
        parser.error("this fixture runs only against a local development API")

    with (ROOT / "docs" / "demo-incidents.csv").open(newline="", encoding="utf-8") as f:
        row = next(item for item in csv.DictReader(f) if item["id"] == "2")
    incident = {"title": "CyberGuard demonstration: suspicious email",
                "description": row["description"], "affected_system": "Corporate email"}
    if not args.apply:
        print("DRY RUN - one synthetic incident:")
        print(json.dumps(incident, indent=2))
        print("Use --apply --password-file <secure path> to create it explicitly.")
        return
    if MARKER.exists() and not args.force:
        print("Already submitted from this checkout:", json.loads(MARKER.read_text())["incident_id"])
        return
    if args.password_file is None:
        parser.error("--password-file is required with --apply")
    password = args.password_file.read_text(encoding="utf-8").rstrip("\r\n")
    opener = build_opener(HTTPCookieProcessor(CookieJar()))
    origin = args.api.removesuffix("/api")

    def post(path, body):
        request = Request(args.api + path, data=json.dumps(body).encode("utf-8"),
                          headers={"Content-Type": "application/json", "Origin": origin}, method="POST")
        with opener.open(request, timeout=25) as response:
            return json.load(response)

    post("/auth/login", {"username": args.user, "password": password})
    response = post("/incidents", incident)
    incident_id = response["incident"]["id"]
    MARKER.parent.mkdir(parents=True, exist_ok=True)
    MARKER.write_text(json.dumps({"incident_id": incident_id}) + "\n")
    print("Created demo incident:", incident_id)


if __name__ == "__main__":
    main()
