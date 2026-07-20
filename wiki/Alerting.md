# 🔔 Alerting

Model Hotel can push outbound notifications for noteworthy operational events (a provider going down, a circuit breaker tripping, a failover group failing to sync) to wherever you want them: Telegram, email, Discord, Slack, Matrix, a raw webhook, and ~80 other destinations.

It does this through [Apprise](https://github.com/caronc/apprise): you run a small, stateless `apprise-api` container, Model Hotel POSTs a short event summary to it, and Apprise fans the notification out to your chosen service. Model Hotel writes no per-service integration code, ships no Python in its image, and (consistent with its [[Privacy]] stance) **never sends request or response content**, only the event summary (e.g. "Provider openai circuit breaker: open").

---

## Table of Contents

- [How it works](#how-it-works)
- [Setup](#setup)
- [Choosing which events fire](#choosing-which-events-fire)
- [Notification targets](#notification-targets)
- [Phone push (ntfy and Bellhop)](#phone-push-ntfy-and-bellhop)
- [Security](#security)
- [Reliability](#reliability)

---

## How it works

```
 events bus  ──►  internal/alert dispatcher  ──►  POST  ──►  apprise-api  ──►  your service
 (circuit breaker,     (filter ∩ your picker,                 (you run it)      (Telegram, …)
  failover, discovery)  debounce, single POST)
```

The dispatcher is a single consumer of Model Hotel's internal event bus. For each event it checks: is this event in the catalog, is alerting enabled and configured, did you select this event, and has an identical alert not just fired (debounce). If all pass, it sends one `POST` to `{apprise_api_url}/notify` with a title, body, and severity. Everything else (the 80+ service integrations) lives in the Apprise container, maintained upstream.

## Setup

1. **Run an `apprise-api` container.** The bundled `docker-compose.yml` ships a commented `apprise` service; uncomment it:

   ```yaml
   apprise:
       image: caronc/apprise:latest
       restart: unless-stopped
       expose:
           - "8000"
   ```

   It is not exposed to the host; only Model Hotel needs to reach it on the internal network.

2. **Configure it in the dashboard.** Open **Settings → Alerts**:
   - Toggle **Enable alerting** on.
   - Set **Apprise API URL** to `http://apprise:8000` (the service name from compose).
   - Paste your **Notification target**: your Apprise URL, e.g. `tgram://<bot_token>/<chat_id>`. Stored encrypted (see [Security](#security)).
   - Click **Send test notification** to verify the whole chain end to end.

A live **reachability indicator** next to the URL shows whether Model Hotel can reach the apprise-api container: green (reachable), amber (reachable but the container reports an issue), or red (unreachable, e.g. wrong URL or the container isn't running), so a misconfiguration is visible immediately rather than only when an event later fails to send. Use **Re-check** to re-probe.

![Settings Alerts](screenshots/settings_alerts.png)
*Settings page - Alerts section. With alerting off, the "Events to notify on" column stays visible but disabled; enabling the toggle activates the target field, reachability indicator, and the event picker.*

## Choosing which events fire

The **Events to notify on** picker (expand it under the target field) lists every event you can subscribe to, grouped by category, each with a severity dot. Toggle individual events or whole categories. The list is served by the backend catalog (`GET /api/alert/events`), so it always reflects exactly what the running version can emit.

Current events:

| Event | Category | Default | Fires when |
|---|---|---|---|
| Provider down (circuit breaker opened) | Failover | ✅ on | a provider's breaker trips |
| Provider recovered (circuit breaker closed) | Failover | ✅ on | the breaker recovers |
| Provider being probed (half-open) | Failover | ⬜ off | the breaker enters its probe state (noisy) |
| Failover group sync failed | Failover | ✅ on | a failover group fails to sync |
| Provider failed during discovery | Discovery | ⬜ off | a provider errors during model discovery |
| Fleet ownership conflict | High Availability | ✅ on | a second Front Desk tries to claim a member that another Front Desk already owns (debounced to once/hour per rejected Front Desk id) |

On first run the default-on events are pre-selected. Deselecting everything means nothing fires.

## Notification targets

The target is any [Apprise URL](https://AppriseIt.com/services/). The Alerts section shows copyable examples for popular services; a few:

| Service | URL shape |
|---|---|
| Telegram | `tgram://{bot_token}/{chat_id}` |
| Discord | `discord://{webhook_id}/{webhook_token}` |
| Slack | `slack://{tokenA}/{tokenB}/{tokenC}` |
| Email | `mailto://{user}:{password}@gmail.com` |
| Webhook (JSON) | `json://{host}/{path}` |

Send to multiple destinations at once by separating Apprise URLs with `;`.

## Phone push (ntfy and Bellhop)

Alerts can reach your phone with no Google services, using [ntfy](https://ntfy.sh) as the delivery channel. This is also what powers real-time push in [[Bellhop]]. It is the same Apprise pipeline as any other target, with two extra pieces: an `apprise-api` container for the sending side to POST to, and an ntfy topic your phone subscribes to.

You do **not** run an "ntfy.sh" container. `ntfy.sh` is a public hosted service, so the only container you add is `apprise-api` (you may optionally self-host an ntfy server instead, see below). The chain is:

```
 Front Desk event  ──►  apprise-api  ──►  ntfy.sh/<your-topic>  ──►  phone (ntfy app / Bellhop)
```

**1. Add `apprise-api` to the Front Desk stack.** For Bellhop the alerts come from Front Desk (fleet and member events), so the container belongs with Front Desk, not the main gateway; adding it to the main `docker-compose.yml` would only wire the gateway's own alerts. The `deploy/ha/docker-compose.yml` stack ships it commented out; uncomment it:

```yaml
services:
  frontdesk:
    # ... existing Front Desk service ...

  apprise:
    image: caronc/apprise:latest
    restart: unless-stopped
    # Internal only; Front Desk reaches it as http://apprise:8000. No host port needed.
    expose:
      - "8000"
```

**2. Point Front Desk at it and at your ntfy topic.** In Front Desk → **Settings → Alerts**:
   - Enable alerting and set **Apprise API URL** to `http://apprise:8000`.
   - In the **Phone push via ntfy** helper, enter the ntfy server (`https://ntfy.sh`) and a **secret topic** name (treat it like a password: anyone who knows it can read your alerts), then **Set as target**. This composes the Apprise URL `ntfys://ntfy.sh/<topic>` for you.
   - **Send test notification** to verify the whole chain.

**3. Subscribe on the phone.** Install the [ntfy Android app](https://ntfy.sh) and subscribe to the same topic, or use [[Bellhop]]'s real-time push, which registers the topic for you (see the Bellhop page for the phone-side steps).

**Self-hosting ntfy.** If you would rather not use the public server, run your own ntfy container. Unlike `apprise-api`, it must be **publicly reachable** (the phone connects to it from anywhere), so put it behind your TLS reverse proxy. Then enter your server's URL (e.g. `https://ntfy.example.com`) in the helper: `https` servers compose to `ntfys://…`, plain `http` to `ntfy://…`.

## Security

The notification target typically contains a credential (a bot token, an SMTP password). Model Hotel **encrypts it at rest** with the same `MASTER_KEY`-derived scheme used for provider API keys, and the dashboard only ever shows a masked placeholder; the stored value is never returned to the browser. To change it, type a new value; to keep it, leave the field untouched.

## Reliability

Alerting is strictly **best-effort and non-blocking**. A missing, misconfigured, or failing `apprise-api` never affects request serving and never fails a proxied request; failures are logged and dropped. A per-event, per-provider debounce window suppresses repeat alerts so a flapping circuit breaker cannot spam you; recovery ("all clear") notifications are always delivered.

---

See also: [[Failover and Hotel Routing]] · [[Request Logging]] · [[Privacy]]
