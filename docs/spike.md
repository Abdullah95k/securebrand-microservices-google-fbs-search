# Spike: which search source finds new Facebook pages?

Before the daily discovery tool is built, this spike measures each
candidate source on the same sample of fbs keywords. It reports, per source:

- queries sent, and how they ended (answered, CAPTCHA, bot check, rate limited, failed);
- results per query, and how many are on facebook.com;
- how many of those map to a page or account (and why the others were skipped: group, event, Marketplace, no owner in the URL...);
- distinct pages and accounts found, and how many fbs doesn't know yet;
- the same per language (`en`, `ar`, `ckb`, `kmr`);
- the overlap between sources.

The report holds counts only. The URLs found go to a separate results file
on the server (mode 600), with only the URL, the page or account, the
keyword and the time. Delete it when you are done.

All times are Iraq time (UTC+3).

## Safety rules built in

- **CAPTCHAs and bot checks:** the first one stops the run for the day. Nothing tries to solve or get around it, and no query is retried.
- **APIs that say "too many requests":** the query is dropped, the run pauses 2, then 4 minutes, and stops after 3 in a row.
- **Credentials** are read from files on the server and never printed, logged or committed. The proxy file is read where it is (`../securebrand-microservices/phase0/proxies.yaml`) and never copied; proxies show up as "exit 1", "exit 2"...
- **Caps:** at most 200 queries per API source, and 12 for the Google probe.

## What it costs

| Part | Cost | Time |
|---|---|---|
| `make spike-checks`: facebook.com robots.txt and Common Crawl | free | 1-3 minutes |
| Brave Search API | about 80 queries; Brave gives $5 of credit a month (about 1,000 queries) | about 7 minutes |
| Mojeek API (optional) | about 40 queries, at £1-3 per 1,000 | about 3 minutes |
| Vertex AI Search (optional) | about 40 queries, at a few dollars per 1,000 | about 3 minutes |
| Google probe (optional, read the risks first) | free, at most 12 queries | up to 1 hour |

## Steps

### 1. Connect and get the code

```bash
ssh root@YOUR_SERVER_IP
```

```bash
cd ~ && git clone https://github.com/Abdullah95k/securebrand-microservices-google-fbs-search.git
```

```bash
cd ~/securebrand-microservices-google-fbs-search
```

If it is already there, update it instead:

```bash
cd ~/securebrand-microservices-google-fbs-search && git pull --ff-only
```

Go is already installed (Phase 0 used it). Check:

```bash
go version
```

### 2. The free checks

```bash
make spike-checks
```

It reads facebook.com's robots.txt (which crawlers Facebook lets in) and
asks Common Crawl how much of facebook.com its latest monthly crawl holds.

### 3. An fbs API key for the spike

The pilot must be running. This creates a read-only key (`data:read`) and
saves it into `secrets/fbs_api_key` without showing it:

```bash
make fbs-key
```

### 4. A Brave Search API key

1. Sign up at <https://api-dashboard.search.brave.com>.
2. Choose the **Search** plan. Brave asks for a card; the monthly $5 credit covers about 1,000 queries, and the spike uses about 80.
3. Create an API key and copy it.

Save it on the server (paste it at the prompt; nothing is shown):

```bash
make secret NAME=brave_api_key
```

### 5. Optional: Mojeek and Vertex AI Search

Mojeek: buy API access at <https://www.mojeek.com/services/search/web-search-api/>, then:

```bash
make secret NAME=mojeek_api_key
```

Vertex AI Search is Google's own paid replacement for the Custom Search API.
It needs a Google Cloud project with billing, a website search app whose
data store lists `www.facebook.com/*`, and an API key allowed to call the
Discovery Engine API. Save the key, then the app's serving config path
(it looks like `projects/<project>/locations/global/collections/default_collection/engines/<app>/servingConfigs/default_search`):

```bash
make secret NAME=vertex_api_key
```

```bash
make secret NAME=vertex_serving_config
```

### 6. Measure the APIs

It runs every source that has a key file, on 40 keywords (10 per
language), for results of the past week and of any time:

```bash
make spike-apis
```

### 7. Optional: the Google probe

**Read this first.** Google's terms of service don't allow automated
queries. The risks:

- Google may answer with CAPTCHAs and block the proxy IPs for Google for a while. The same IPs carry fbs's Facebook traffic; Google blocking them doesn't affect Facebook, but check that your proxy provider's terms allow Google traffic.
- In principle Google could treat it as a breach of its terms. In practice, at this volume, the usual result is blocking, not legal action; this is not legal advice.
- Since January 2025 Google shows its results only to browsers that run JavaScript. A plain request gets a "turn on JavaScript" page. That page is a bot check, so by the rules above the probe stops there and does not use a browser to get past it. Expect it to stop after the first query.

The probe sends at most 12 queries, one every 3 to 5 minutes, through the
ISP proxies of `phase0/proxies.yaml`, and stops at the first CAPTCHA or bot
check. To run it in the background, so that it survives the SSH session
closing:

```bash
mkdir -p out && nohup make spike-google > out/spike-google.log 2>&1 &
```

Check on it (it takes up to an hour):

```bash
tail -n 30 out/spike-google.log
```

### 8. Send back the numbers

The reports are in `out/`. They hold counts only, no URLs and no keys, so
they are safe to paste into the chat:

```bash
cat out/report-*.txt
```

Then delete the results files (they hold the URLs found):

```bash
rm -f out/results-*.csv
```

## Changing the sample

`SPIKE_ARGS` passes options through. For example, 20 keywords per language
and a different random sample:

```bash
make spike-apis SPIKE_ARGS="--per-lang 20 --seed 2"
```

Every option: `go run ./cmd/gfs-spike apis -h` (or `checks -h`, `google -h`).
