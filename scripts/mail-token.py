#!/usr/bin/env python3
"""Print the password-reset token from the newest message the catcher holds.

Takes the catcher's API base URL, not a dump: the message list carries no body,
so the body has to be fetched for the newest id, and doing that here keeps the
caller to one line.

An empty mailbox is an answer, not a crash. The previous version indexed
straight into the list and raised IndexError, which read as a broken script
rather than as "no mail arrived" — the thing the check exists to detect.
"""
import json
import re
import sys
import urllib.error
import urllib.request


def fetch(url):
    with urllib.request.urlopen(url, timeout=5) as r:
        return json.load(r)


def main():
    if len(sys.argv) < 2:
        print("usage: mail-token.py <catcher-api-base-url>", file=sys.stderr)
        return 2

    base = sys.argv[1].rstrip("/")
    try:
        listing = fetch(base + "/api/v1/messages?limit=1")
    except (urllib.error.URLError, TimeoutError) as e:
        print(f"no answer from the mail catcher at {base}: {e}", file=sys.stderr)
        return 1

    messages = listing.get("messages") or []
    if not messages:
        print("the mail catcher holds no message", file=sys.stderr)
        return 1

    try:
        body = fetch(base + "/api/v1/message/" + messages[0]["ID"])
    except (urllib.error.URLError, TimeoutError, KeyError) as e:
        print(f"could not read the newest message: {e}", file=sys.stderr)
        return 1

    # Plain text first; the HTML part carries the same link, folded differently.
    for part in (body.get("Text") or "", body.get("HTML") or ""):
        m = re.search(r"/reset\?token=([A-Za-z0-9_\-]+)", part)
        if m:
            print(m.group(1))
            return 0

    print("the newest message carries no reset link", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
