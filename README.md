# securebrand-microservices-google-fbs-search

Finds public Facebook pages and accounts for
[fbs](https://github.com/Abdullah95k/securebrand-microservices) through web
search, since Facebook's own search needs a login. Each day it will read
fbs's discovery keywords, search for facebook.com results, turn each result
into the page or account behind it, drop what fbs already has, and hand the
new ones to fbs.

**Status:** spike. Before the tool is built, `gfs-spike` measures which
search source is worth it: see [docs/spike.md](docs/spike.md).

## What is here

| Path | What it does |
|---|---|
| `cmd/gfs-spike` | The spike: `checks` (robots.txt, Common Crawl), `apis` (Brave, Mojeek, Vertex AI Search), `google` (opt-in probe) |
| `internal/fburl` | facebook.com URL → page or account reference (`v:<vanity>` or `id:<n>`, as fbs writes them); skips groups, events, Marketplace and URLs that don't name their owner |
| `internal/source` | One adapter per search source; CAPTCHAs and bot checks are reported, never worked around |
| `internal/spike` | Keyword sampling, pacing, stop rules, counts and the report |
| `internal/fbs` | fbs API client (keywords, known pages) |
| `internal/secret` | Credentials from files; never printed, and kept out of error messages |
| `internal/proxies` | Reads fbs's proxy file in place; exits are named "exit N", never by address |
| `internal/checks` | robots.txt and Common Crawl index checks |

## Rules

- Never commit credentials, proxy addresses, real data or result files. `secrets/` and `out/` are git-ignored, and CI fails if such files are tracked.
- Store only the result URL, the page or account reference, the keyword and the time. No post text, no titles.
- A CAPTCHA or bot check stops the run: no solving, no retries, no workarounds.

## Development

```bash
make check    # build, tests, tests with the race detector, golangci-lint
```
