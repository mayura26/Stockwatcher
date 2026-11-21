# Stockwatcher

A private, mobile-first blotter for US stocks, CME futures, crypto, and Yahoo indexes such as Treasury yields and VIX. The phone reads SQLite. Finnhub, Yahoo, and Binance are only touched by the server and by the scheduled tick.

## Run locally

```bash
./run.sh
```

On Windows you can double-click `run.bat`. Both start the server at http://127.0.0.1:8080. With no `.env`, the access token is `devtoken`.

Copy `.env.example` to `.env` to set `ACCESS_TOKEN`, `FINNHUB_API_KEY`, and the VAPID keys. `./run.sh vapid` prints a key pair. `./run.sh tick` refreshes quotes and evaluates alerts once.

`DB_PATH` defaults to `data/stockwatcher.db`.

## Coolify

Build with the Dockerfile. Mount a persistent volume at `/data`.

| Variable | Purpose |
| --- | --- |
| `ACCESS_TOKEN` | Bearer token the PWA asks for on first open |
| `FINNHUB_API_KEY` | US stock quotes and search |
| `DB_PATH` | `/data/stockwatcher.db` |
| `ADDR` | `:8080` |
| `VAPID_PUBLIC_KEY` | From `stockwatcher vapid` |
| `VAPID_PRIVATE_KEY` | From `stockwatcher vapid` |
| `VAPID_SUBSCRIBER` | A contact email, for example `you@example.com` |

Start command: `stockwatcher serve` (the image entrypoint already drops to uid 65532).

Scheduled task, every minute, inside the app container:

```bash
su-exec 65532:65532 stockwatcher tick
```

Run it as uid 65532 so the tick and the server share the SQLite file. The task evaluates price crosses and sends Web Push. Futures and Yahoo indexes (Treasury yields, VIX) come from Yahoo and are delayed about 10–15 minutes, so those alerts fire on that delay. Crypto is Binance. US stock quotes, including ETFs such as SPY and QQQ, are Finnhub.

Health check: `GET /healthz`.

## Push on a phone

The site must be HTTPS. Install the PWA (on iPhone: Share, Add to Home Screen; iOS 16.4 or later), open it from the icon, and tap Enable alerts.
