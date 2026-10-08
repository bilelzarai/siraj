# Sirāj — the public endpoint

One read-only route, credentialed, versioned. It serves the same questions the
game draws from, under the same conditions, and it is the only way into this
system that is not a browser session.

The machine-readable version of this page is [`api/openapi.yaml`](../api/openapi.yaml).
Where the two differ, the handler is right and both are wrong.

## Credential

A bearer key, minted from the command line and never through the web:

```bash
sirajctl apikey new "mirror for the mobile app"   # shown once, never recoverable
sirajctl apikey list                              # labels, creation, last use
sirajctl apikey revoke 3                          # refused from the next request
```

Only the hash of a key is stored. Losing one means minting another; there is no
path, through this tool or the database, that hands a key back.

```
Authorization: Bearer siraj_…
```

| Answer | When |
|---|---|
| `401` | No header, or a key this system does not know. Deliberately the same answer to both: telling a caller that a key exists is telling them a key exists |
| `403` | A key that has been revoked. "This credential is finished" is a different thing to be told |
| `429` | 120 requests a minute per key. `Retry-After` says when |

## `GET /api/v1/questions`

| Parameter | Default | Notes |
|---|---|---|
| `locale` | `ar` | One of `ar`, `en`, `fr`. A question is served only in a language it has been translated into and reviewed in |
| `category` | every | Category slug |
| `domain` | every | Subject-area slug — the level above a category |
| `difficulty` | every | `1`, `2` or `3` |
| `limit` | 50 | Capped at 200 by the server |
| `cursor` | — | From the previous response. Opaque: it is this endpoint's business what is inside it |

```json
{
  "data": [
    {
      "id": 101,
      "category": { "slug": "quran", "name": "The Noble Qur'an", "icon": "📖" },
      "domain":   { "slug": "islamic", "name": "Islamic Knowledge", "icon": "🕌" },
      "difficulty": 1,
      "points": 10,
      "prompt": "How many surahs are there in the Qur'an?",
      "choices": ["110", "112", "114", "116"],
      "correct_index": 2,
      "explanation": "The Qur'an contains 114 surahs.",
      "locale": "en"
    }
  ],
  "next_cursor": "aWQ6MTAy",
  "total": 65
}
```

`data` is `[]` and never `null`. `next_cursor` is present only when a full page
came back, so a cursor never leads to an empty one. `total` counts what the
filter matches, under the same conditions as the page.

## What it will not serve

- **Anything awaiting review.** Machine translations and imports are held until a human approves them, and this endpoint inherits that rule rather than restating it. A question is absent in a language it has not been reviewed in, even though it is present in another.
- **A retired category, or a category inside a retired subject area.** The same five conditions the game's own draw applies.
- **A question a player wrote.** Player-authored sets are private to the people invited to play them.
- **Anything, to a request without a key.** The answer sheet is the asset: correct answers at scale is a product decision, which is the whole reason this surface is credentialed (D2).

## Versioning

The prefix is the version. A field may be added; one is never removed, renamed
or given a new meaning without `/api/v2`. The surface is read-only, and gaining
a mutation would mean a new version and a written contract first.
