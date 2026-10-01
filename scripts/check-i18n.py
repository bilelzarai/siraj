#!/usr/bin/env python3
"""Cross-check every T("key", ...) call site against the catalog.

A key whose text has one %s but is called with no arguments renders the raw
"%s" to the user; the reverse renders "%!(EXTRA string=...)". Neither fails
the build or the tests, so this check exists to catch them.
"""
import json, re, glob, sys, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
en = json.load(open(os.path.join(ROOT, 'internal/i18n/locales/en.json'), encoding='utf-8'))


def verb_count(s):
    n, i = 0, 0
    while i < len(s) - 1:
        if s[i] == '%':
            if s[i + 1] == '%':
                i += 2
                continue
            n += 1
        i += 1
    return n


def arg_count(src, start):
    """Count arguments from just after the key literal to the closing paren."""
    depth, i, args = 1, start, 1
    while i < len(src) and depth > 0:
        ch = src[i]
        if ch in '([{':
            depth += 1
        elif ch in ')]}':
            depth -= 1
            if depth == 0:
                break
        elif ch == ',' and depth == 1:
            args += 1
        i += 1
    return args


call = re.compile(r'\.T\(\s*"([a-zA-Z0-9_.]+)"\s*(,)?')
problems = []

for path in sorted(glob.glob(os.path.join(ROOT, 'internal/**/*.templ'), recursive=True) +
                   glob.glob(os.path.join(ROOT, 'internal/**/*.go'), recursive=True)):
    rel = os.path.relpath(path, ROOT)
    # The i18n package is the implementation and its own tests: its T() calls
    # take a locale first, so they do not follow the call shape checked here.
    if path.endswith('_templ.go') or rel.startswith('internal/i18n/'):
        continue

    src = open(path, encoding='utf-8').read()
    for m in call.finditer(src):
        key, has_args = m.group(1), bool(m.group(2))
        # A key built by concatenation ends at a dot; it cannot be checked
        # here.
        #
        # This is also why there is no "unused key" report. Keys are reached
        # through prefixes (`notifications.kind.` + the event), through
        # variables (`key := "upload.failed"`), and through helpers that
        # return one. A search for a key's literal text finds none of those,
        # so deleting what the search misses is how a status silently starts
        # rendering as its own name. Never prune this catalogue by grep.
        if key.endswith('.'):
            continue
        if key not in en:
            problems.append(f'{rel}: unknown key "{key}"')
            continue
        want = verb_count(en[key])
        got = arg_count(src, m.end()) if has_args else 0
        if got != want:
            problems.append(
                f'{rel}: "{key}" expects {want} argument(s), call passes {got}')

# Every locale carries every key, with the same holes in the same places.
#
# The call-site check above only ever reads English, so a key added to en.json
# and forgotten in the others passed it — and a missing key renders as its own
# name, which is the kind of bug that ships because nobody on the team reads
# the language it happens in.
for path in sorted(glob.glob(os.path.join(ROOT, 'internal/i18n/locales/*.json'))):
    loc = os.path.splitext(os.path.basename(path))[0]
    if loc == 'en':
        continue
    other = json.load(open(path, encoding='utf-8'))
    for key, text in en.items():
        if key not in other:
            problems.append(f'{loc}.json: missing "{key}"')
        elif verb_count(other[key]) != verb_count(text):
            problems.append(
                f'{loc}.json: "{key}" takes {verb_count(other[key])} argument(s), '
                f'English takes {verb_count(text)}')
    for key in other:
        if key not in en:
            problems.append(f'{loc}.json: "{key}" is not in English')

if problems:
    for p in sorted(set(problems)):
        print('i18n:', p)
    sys.exit(1)

print(f'i18n: {len(en)} keys, all call sites match')
