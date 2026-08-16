# 🔔 Alerting

Model Hotel can push outbound notifications for noteworthy operational events (a provider going down, a circuit breaker tripping, a failover group failing to sync) to wherever you want them: Telegram, email, Discord, Slack, Matrix, a raw webhook, and ~80 other destinations.

It does this through [Apprise](https://github.com/caronc/apprise): you run a small, stateless `apprise-api` container, Model Hotel POSTs a short event summary to it, and Apprise fans the notification out to your chosen service. Model Hotel writes no per-service integration code, ships no Python in its image, and (consistent with its [[Privacy]] stance) **never sends request or response content**, only the event summary (e.g. "Provider openai circuit breaker: open").

---

## Table of Contents

- [How it works](#how-it-works)
- [Setup](#setup)
  - [Guided setup (Front Desk)](#guided-setup-front-desk)
  - [Manual configuration](#manual-configuration)
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

2. **Point your alert surface at it.** [[High Availability]]'s Front Desk has a guided wizard that does the whole job (below). The Model Hotel dashboard keeps its manual Alerts card, and gets the same wizard in a follow-up release.

### Guided setup (Front Desk)

Front Desk's **Settings → Alerts** card carries exactly one guided button: **Set up alerts** while nothing is stored, and **Add destination** once a destination is saved. It opens a seven-step wizard (**Add destination** enters at step 2 and skips the Apprise API URL, which is already stored). Every step verifies its own input against the real container before **Next** unlocks, and nothing is written until **Finish**, so **Cancel** at any point leaves your settings exactly as they were.

![Front Desk Alerts card](screenshots/frontdesk_settings_alerts.png)
*Front Desk Settings - Alerts: the status pill, the plaintext Destinations list with per-row Copy, Test and Remove, the single guided button (**Add destination** on a configured card, **Set up alerts** on a fresh one), and the individual fields folded into "Manual configuration (advanced)".*

1. **Apprise.** The **Apprise API URL**, prefilled `http://apprise:8000`, which is the `apprise` service of the Front Desk stack in `deploy/ha/docker-compose.yml` (it ships commented out; uncomment it as shown under [Phone push](#phone-push-ntfy-and-bellhop)). Front Desk sends its own fleet and member alerts, so the container belongs beside it rather than in the main gateway's `docker-compose.yml`. **Check** probes the container and the step unlocks on "apprise-api reachable and healthy". A red result names the reason: nothing answered at that address, the container answered but reports a problem, or the URL is not valid.
2. **Where should alerts go?** Tiles for **Phone (ntfy app)**, **Bellhop**, **Telegram**, **Discord**, **Email** and **Other (Apprise URL)**.
3. **Details.** Plain fields for the tile you picked, with the composed Apprise URL rendered in clear underneath. You never type an Apprise URL yourself.
4. **Test this destination.** **Send test** posts one notification through the step-1 container to this destination only, and nothing is saved. A failure says which part failed: apprise rejected the destination URL, apprise could not deliver it (check the server and topic, or the bot token), or apprise stopped answering. A Bellhop destination answers the test with a "push test received" notification on the phone (needs Bellhop 0.9.10 or newer); real alerts only wake it.
5. **Destinations.** The destinations this run adds; anything already saved stays as it is. **Add another** returns to step 2. The wizard only appends; it never drops a destination you already had.
6. **Events.** The event picker. A setup run starts from the recommended set; a run that adds a destination to a configured Front Desk starts from the selection you already saved, including an empty one. **Reset to recommended** is always there to tick the recommended set again.
7. **Finish.** One write covering the API URL, the destinations, the events and the enable toggle, so a failure leaves nothing half-applied. The final screen offers **Send test to everything**.

![Alerts wizard, step 1](screenshots/frontdesk_alerts_wizard_step1.png)
*Step 1: the Apprise API URL after a successful Check. Next stays locked until the container answers.*

![Alerts wizard, step 3](screenshots/frontdesk_alerts_wizard_step3_ntfy.png)
*Step 3 for the Phone (ntfy app) tile: server and topic as plain fields, Generate for a random topic, the subscribe instructions for the phone with copy buttons, and the composed Apprise URL underneath. Whether the server is reachable is settled by step 4, which sends from Front Desk rather than from the browser.*

![Alerts wizard, step 4](screenshots/frontdesk_alerts_wizard_step4_test.png)
*Step 4: one test to the destination being added, through the step-1 container. Still nothing saved.*

### Manual configuration

On the Model Hotel dashboard the individual fields are the whole surface. Open **Settings → Alerts**:

- Turn **Enable alerting** on.
- Set **Apprise API URL** to `http://apprise:8000` (the service name in the gateway's own compose block above).
- Paste your **Notification target**: one or more Apprise URLs separated by `;`, e.g. `tgram://<bot_token>/<chat_id>`. Stored encrypted (see [Security](#security)).
- Click **Send test notification** to verify the whole chain end to end.

Front Desk offers the same fields under its **Manual configuration (advanced)** disclosure, below the Destinations list, for adjusting a saved setup without walking the wizard again.

A live **reachability indicator** next to the URL shows whether the gateway can reach the apprise-api container: green (reachable), amber (reachable but the container reports an issue), or red (unreachable, e.g. wrong URL or the container isn't running), so a misconfiguration is visible immediately rather than only when an event later fails to send. Use **Re-check** to re-probe.

![Settings Alerts](screenshots/settings_alerts.png)
*Model Hotel dashboard - Alerts section with alerting on: the Apprise API URL with its reachability indicator, the notification target, the ntfy helper, and the "Events to notify on" picker unrolled. With alerting off the card rolls up to its toggle.*

## Choosing which events fire

The **Events to notify on** picker (step 6 of the wizard, or under the target field in the manual fields) lists every event you can subscribe to, grouped by category, each with a severity dot. Toggle individual events or whole categories. The list is served by the backend catalog (`GET /api/alert/events`), so it always reflects exactly what the running version can emit.

Current events:

| Event | Category | Default | Fires when |
|---|---|---|---|
| Provider down (circuit breaker opened) | Failover | ✅ on | a provider's breaker trips |
| Provider recovered (circuit breaker closed) | Failover | ✅ on | the breaker recovers |
| Failover group sync failed | Failover | ✅ on | a failover group fails to sync |
| Provider failed during discovery | Discovery | ⬜ off | a provider errors during model discovery |
| Fleet ownership conflict | High Availability | ✅ on | a second Front Desk tries to claim a member that another Front Desk already owns (debounced to once/hour per rejected Front Desk id) |
| SSO identity bound | Security | ⬜ off | an external identity is bound to an admin account for the first time |
| Quota schema drift | Quota | ✅ on | a provider changes the *shape* of its quota response (a key path appears or disappears). Carries the added and removed paths. Alert-only: nothing about routing or failover changes, but a normalizer written against the old shape may now be reporting the wrong numbers silently, which is why it defaults on |

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

Send to multiple destinations at once by separating Apprise URLs with `;`. The wizard composes and appends them for you; the manual field takes the joined string.

On Front Desk the saved targets also appear above the manual fields as a **Destinations** list, one row per destination with its kind, host and secret segment in clear, plus **Copy**, **Test** (sends to that one destination) and **Remove**. The list is admin-only, and it is plaintext on purpose: anyone who can read the page can already change what it holds, so hiding the string behind asterisks only makes it harder to check what is actually stored.

## Phone push (ntfy and Bellhop)

Alerts can reach your phone with no Google services, using [ntfy](https://ntfy.sh) as the delivery channel. This is also what powers real-time push in [[Bellhop]]. It is the same Apprise pipeline as any other target, with two extra pieces: an `apprise-api` container for the sending side to POST to, and an ntfy topic your phone subscribes to.

You pick the ntfy server. Self-host one (see below) or use the public `ntfy.sh`; neither is assumed, and nothing prefills a server for you. Either way the only container you add is `apprise-api`: `ntfy.sh` is a hosted service, and a self-hosted ntfy is a container of its own. The chain is:

```
 Front Desk event  ──►  apprise-api  ──►  <your ntfy server>/<your-topic>  ──►  phone (ntfy app / Bellhop)
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

**2. Run the wizard for the ntfy app.** In Front Desk → **Settings → Alerts**, press **Set up alerts** (or **Add destination** if alerts are already configured), then:
   - Step 1: **Apprise API URL** `http://apprise:8000`, then **Check** until it reports the container reachable and healthy.
   - Step 2: pick the **Phone (ntfy app)** tile.
   - Step 3: enter your ntfy server (yours, or `https://ntfy.sh`) and press **Generate** for a random 20-character topic. The topic is the only access control on a public server, so treat it like a password; **Generate** exists so you do not invent a weak one. The step also prints the exact phone-side steps ("Subscribe to topic, Use another server", then the server and topic with copy buttons) and shows the composed `ntfys://<server>/<topic>` underneath.
   - Step 4: **Send test**. Subscribe on the phone first (step 3 below) and the test lands there.
   - Steps 5 to 7: add more destinations if you want, pick events, **Finish**.

**3. Subscribe on the phone.** Install the [ntfy Android app](https://ntfy.sh) and subscribe to the same server and topic, or use [[Bellhop]]'s real-time push, which generates the topic on the phone instead; the wizard's **Bellhop** tile then takes it as a single paste (see the Bellhop page for the phone-side steps).

**Bellhop instead of the ntfy app.** [[Bellhop]] generates its own topic on the phone, so there is nothing to invent on the Front Desk side. Turn on **Real-time push** in Bellhop, copy the **Push topic for Front Desk** it shows, and paste that one string into the wizard's **Bellhop** tile: Front Desk splits it into server and topic and composes the Apprise URL. A test through that destination shows a "push test received" notification in Bellhop, so you can see the pipeline works end to end. Real alerts only wake Bellhop: it notifies from its own fleet poll (a member going down or recovering, auto-sync drifting) rather than from the push payload.

**Self-hosting ntfy.** If you would rather not use the public server, run your own ntfy container. Unlike `apprise-api`, it must be **publicly reachable** (the phone connects to it from anywhere), so put it behind your TLS reverse proxy. Then enter your server's URL (e.g. `https://ntfy.example.com`) in the wizard or in the manual ntfy helper: `https` servers compose to `ntfys://…`, plain `http` to `ntfy://…`.

## Security

The notification target typically contains a credential (a bot token, an SMTP password). Both surfaces **encrypt it at rest** with the same `MASTER_KEY`-derived scheme used for provider API keys (Front Desk with its own `FRONTDESK_MASTER_KEY`). What they show differs:

- On the **Model Hotel dashboard** the field only ever shows a masked placeholder, and the stored value is never returned to the browser. To change it, type a new value; to keep it, leave the field untouched.
- On **Front Desk** the **Destinations** list shows each saved destination in clear to signed-in admins, for the reason given under [Notification targets](#notification-targets): an admin can already rewrite the targets and fire a test at them, so masking hides nothing from them and only makes it harder to check what is stored. `GET /api/alert/targets` is the one endpoint that decrypts, and it is admin-only.

## Reliability

Alerting is strictly **best-effort and non-blocking**. A missing, misconfigured, or failing `apprise-api` never affects request serving and never fails a proxied request; failures are logged and dropped. A per-event, per-provider debounce window suppresses repeat alerts so a flapping circuit breaker cannot spam you; recovery ("all clear") notifications are always delivered.

A dropped alert is not retried. The dispatcher dials `apprise-api` through the [netguard](Security#netguard-admin-configured-endpoints) client, which allows the private and loopback addresses a notification container actually lives on and blocks link-local/metadata ones. The pre-connection retry the SSO login paths use is deliberately not applied here: nobody is waiting inside a notification, and the next event will alert anyway.

---

See also: [[Failover and Hotel Routing]] · [[Request Logging]] · [[Privacy]]
