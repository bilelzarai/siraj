#!/usr/bin/env python3
"""Print the reset token from the newest message in a MailHog dump.

The mail is multipart/alternative with base64 parts folded at 76 characters,
so the URL is split across lines and cannot be grepped out of the raw body.
"""
import base64, json, re, sys

body = json.load(open(sys.argv[1]))['items'][0]['Content']['Body']

text = ''
for part in re.split(r'--[A-Za-z0-9_\-]+\r?\n', body):
    if 'base64' not in part:
        continue
    # Headers and body are separated by a blank line, CRLF on the wire.
    blob = re.sub(r'[^A-Za-z0-9+/=]', '', re.split(r'\r?\n\r?\n', part, maxsplit=1)[-1])
    try:
        text += base64.b64decode(blob).decode('utf-8', 'ignore')
    except Exception:
        pass

m = re.search(r'/reset\?token=([A-Za-z0-9_\-]+)', text)
print(m.group(1) if m else '')
