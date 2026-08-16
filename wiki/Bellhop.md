# Bellhop (Android Companion App)

Bellhop is the Android companion app for [[Front Desk|High Availability]]. Pair a phone once and it becomes a pocket view of your fleet: live member health, request traffic, the fleet event log, and, for operator devices, the controls to drain a member, activate it again, or push a config sync, all from a lock screen notification tap. Bellhop talks only to Front Desk, never to Model Hotel members directly, so it holds no provider credentials and no member admin tokens. It authenticates with its own device token that either side can revoke at any time, and like the rest of Model Hotel it moves only routing and metering metadata, never prompt or response content.

## The app at a glance

<p align="center">
<a href="screenshots/bellhop_dashboard.png"><img src="screenshots/bellhop_dashboard.png" width="240" alt="Bellhop dashboard: linked Front Desk, quota badge strip, two healthy members with traffic sparklines"></a>
<a href="screenshots/bellhop_member.png"><img src="screenshots/bellhop_member.png" width="240" alt="Bellhop member detail: traffic graph, operator controls, recent events"></a>
<a href="screenshots/bellhop_events.png"><img src="screenshots/bellhop_events.png" width="240" alt="Bellhop fleet event log with date-range filter"></a>
</p>

Three surfaces cover day-to-day monitoring. The **dashboard** is the linked home screen: it names the Front Desk you paired with, carries a strip of [provider quota badges](#quota-badges), shows a fleet health banner, and lists every member as a card with its health dot, address, latency, Traefik status, version, a request sparkline, and its latest event. Tapping a card opens the **member detail** screen with a full request-traffic graph, the member's metadata, operator controls, and its recent events. The top-bar log icon opens the fleet-wide **events** screen. A fourth surface never needs opening at all: the [home-screen widget](#the-home-screen-widget) keeps the fleet, its badges and the latest event on your launcher. Tap any screenshot on this page to view it full size.

## Linking a device

Linking always starts on the Front Desk side, so an operator stays in control of which phones can see the fleet and what they can do.

### From Front Desk

<p align="center"><a href="screenshots/frontdesk_settings_devices_pairing.png"><img src="screenshots/frontdesk_settings_devices_pairing.png" width="820" alt="Front Desk Settings, Paired devices: pairing QR and copyable pairing string"></a></p>

Open **Settings**, then **Paired devices**, pick the new device's role, and generate a pairing code. The role sets the permission ceiling that Front Desk enforces on that device's token: a **Monitor** device is read-only, while an **Operator** device can additionally drain and activate members, trigger a config sync, and toggle auto-sync. The code renders as a QR image alongside a copyable pairing string; both carry the same payload (the Front Desk URL, a one-time code, and a display name). Codes are single-use and expire after a few minutes, and the panel dismisses the code on its own once a device pairs with it.

<p align="center"><a href="screenshots/frontdesk_settings_devices.png"><img src="screenshots/frontdesk_settings_devices.png" width="820" alt="Front Desk Settings, Paired devices: one linked device with role and last-seen time"></a></p>

Every paired device stays listed in the panel with its role and last-seen state, and an operator can revoke any of them with one tap, which invalidates that device's token immediately.

### On your phone

<p align="center">
<a href="screenshots/bellhop_pairing.png"><img src="screenshots/bellhop_pairing.png" width="240" alt="Bellhop first run: Link a Front Desk, with Scan QR code and paste options"></a>
<a href="screenshots/bellhop_pairing_filled.png"><img src="screenshots/bellhop_pairing_filled.png" width="240" alt="Bellhop pairing: string parsed, Front Desk target and device name shown"></a>
</p>

On the phone, either tap **Scan QR code** and point the camera at the code, or copy the pairing string and paste it into the field. Bellhop parses it, shows which Front Desk you are about to link so you can confirm it is the one you meant, and lets you rename the device before pairing. Tap **Pair** and Bellhop exchanges the one-time code for a device token. Front Desk returns that token exactly once; Bellhop stores it encrypted at rest with the Android Keystore (AES-GCM) and never displays it again.

### Roles

A device can never do more than its role allows, because Front Desk enforces the ceiling on the token itself rather than trusting the app. A **Monitor** device sees everything (health, traffic, events, and alerts) but cannot change anything. An **Operator** device gets the same read access plus the operator controls, and each of those writes is additionally gated behind a biometric or device-PIN prompt on the phone, so a borrowed or unlocked device still cannot drain a member without the owner present.

## The dashboard

The dashboard is the linked home screen shown at the top of this page. A banner summarizes fleet health at a glance ("All members up", or a count when something is down), and a strip of provider quota badges sits above it. Each member card carries a colored health dot, the member name, the member address, a compact status line (reachability, latency, Traefik state, and running version), a request-traffic sparkline drawn only when the card is on screen to save battery, and the member's most recent event with a relative timestamp.

The primary member's card names its role with a **Primary** badge and, beside it, an **Auto-sync on** or **Auto-sync off** badge saying whether the fleet's members track the primary's config. On a monitor device that badge is a readout; on an operator device it is a button that opens the auto-sync switch in a bottom sheet, so a setting worth changing once in a blue moon does not sit on the dashboard as though it needed watching. Pull to refresh forces an immediate poll.

## Quota badges

<p align="center">
<a href="screenshots/bellhop_quota.png"><img src="screenshots/bellhop_quota.png" width="240" alt="Bellhop quota detail sheet: MiniMax per-model quota with meter bars"></a>
</p>

The badge strip reports what is left of each provider's quota, straight from the same readings the Model Hotel dashboard shows: a short provider code and one headline number per provider, in that provider's own brand colour, packed as tightly as the screen allows. Tap a badge for a **detail sheet** with the full picture: a meter bar per metered reading (with per-model grouping where a provider reports it that way), and the flat facts underneath, such as membership level, parallel-request limit, or reset time. A reading with no ceiling to measure against gets a plain row rather than a bar, since a bar nobody can fill is decoration.

**Badge style** and which providers appear is yours to set, per surface, in **Settings → Quota badges**: reorder the providers, hide the ones you do not care about, and choose whether a badge counts what you have **used** or what is **remaining**. The dashboard and the widget keep separate lists, so a phone can carry eight badges while the widget carries the two you actually glance at.

## Member detail

<p align="center">
<a href="screenshots/bellhop_member.png"><img src="screenshots/bellhop_member.png" width="260" alt="Bellhop member detail: request-traffic graph, operator controls, recent events"></a>
</p>

Tapping a card opens the member. The top of the screen is a **request-traffic graph** over the window you chose in Settings (requests and errors are drawn as separate series with their own legend, and the time axis spans the selected range). Below it sit the member's address, running version, and when it was added to the fleet. The **Operator controls** section (present only on operator devices) offers **Drain** to bleed traffic off a member, **Activate** to bring a drained member back, and **Sync fleet config** to push the primary's config out; each action asks for a biometric confirmation and reports back whether Front Desk accepted it. A **Recent events** list closes the screen, with its own date-range chips so you can narrow it to the last hour or open it up to all time.

## Events

The events screen is the fleet-wide log. A header counts the events in view, and a row of range chips (1h, 24h, 7d, 30d, All) plus a calendar picker scope the list. Each entry shows a human title and one-line summary, a severity (Error, Warning, Info, or Success) shown as a colored edge, and the raw event type with its source and member (for example `health.down` from `frontdesk-poller` on a given member). The log covers member health transitions, version read failures and recoveries, config syncs (manual and automatic), and device pairing and revocation, so it doubles as an audit trail of everything Front Desk noticed.

## The home-screen widget

<p align="center">
<a href="screenshots/bellhop_widget.png"><img src="screenshots/bellhop_widget.png" width="520" alt="Bellhop home-screen widget: Front Desk name, member count, two healthy members with traffic overlays, quota badge strip, latest fleet event"></a>
</p>

The widget answers "is the fleet fine?" without opening anything. It carries the Front Desk's name, a member count badge (2/2 above), a row per member with its health dot and state, the quota badge strip, and the latest fleet event with its time and date. A footer stamps how old the reading is ("as of 09:01") beside a refresh button, because a widget that cannot say when it last spoke to Front Desk is worse than no widget.

**It never polls on its own.** The widget draws whatever the app or the background check last wrote, which is what keeps it free: no extra battery, no extra requests. Tapping refresh forces a read right then, and background monitoring keeps it current on its own schedule. Tapping the widget anywhere else opens the app; tapping a quota badge opens that provider's detail view where there is one to open, and otherwise raises a toast with the provider's full name, since a badge on a widget has no room to spell out "openrouter-personal".

Resize it and the contents follow: member rows stack to the height you give it, and the badge strip repacks to the width, fitting each badge to its own label and marking anything that did not fit with a **+N** so a trimmed strip never pretends to be the whole picture. Two switches under **Settings → Home-screen widget** decide what it carries at all: the per-member **traffic graph** overlay and the **quota badge** strip. Both are worth knowing the cost of, since each adds a read to every background check, and turning one off removes that read rather than merely hiding the result.

## Alerts

<p align="center">
<a href="screenshots/bellhop_settings_alerts.png"><img src="screenshots/bellhop_settings_alerts.png" width="240" alt="Bellhop settings: Alerts card with per-severity badge counts"></a>
<a href="screenshots/bellhop_alerts.png"><img src="screenshots/bellhop_alerts.png" width="240" alt="Bellhop alerts: What Front Desk alerts on, with per-event toggles"></a>
</p>

The **Alerts** screen shows what Front Desk raises alerts for and, on operator devices, lets you change it. Events are grouped (Health, Config Sync, and so on), each with a severity badge and a switch; flipping a switch enables or mutes that alert on Front Desk right away, so the phone acts as a remote control for the fleet's alerting policy rather than just a viewer of it. A **Notification delivery** panel at the top reports whether an outbound channel (such as an Apprise target) is configured on Front Desk. Monitor devices see the same screen read-only.

## Settings

<p align="center">
<a href="screenshots/bellhop_settings.png"><img src="screenshots/bellhop_settings.png" width="240" alt="Bellhop settings: linked Front Desk, hold to copy, home-screen widget switches, time format, traffic graph range"></a>
<a href="screenshots/bellhop_language.png"><img src="screenshots/bellhop_language.png" width="240" alt="Bellhop language picker with system default and ten locales"></a>
</p>

Settings gathers the device-side preferences. **Linked Front Desk** shows which Front Desk you paired with, the name and role you linked as, and the date, and it long-presses to copy. **Hold to copy** toggles whether long-pressing an event or member cell copies it to the clipboard. **Home-screen widget** governs what the widget carries, one switch per block: **Traffic graphs** overlays each member row with its last hour of requests, and **Quota badges** carries the badge strip. Both cost the background check an extra read while they are on, which is why each says so and why each can be turned off on its own. **Time format** picks the clock every time in Bellhop is drawn on (follow the device, or force 24-hour or 12-hour). **Traffic graph range** sets how far back the request charts reach (1h, 3h, 6h, 12h, or 24h). **App lock** requires a fingerprint or device PIN to open Bellhop at all. **Background monitoring** checks the fleet every fifteen minutes and notifies you when a member goes down or recovers, even while the app is closed. **Real-time push** wakes Bellhop the instant Front Desk pushes an alert, over UnifiedPush and ntfy, with no Google dependency and no polling delay; it is opt-in. **Battery** reports whether Android is letting Bellhop run in the background at all, and offers to fix it when it is not. **Quota badges** opens the badge picker described above, **Alerts** opens the alert policy, **Language** offers the system default plus ten hand-translated locales, and the screen ends with **Unlink**.

## Notifications and background monitoring

Bellhop keeps you informed with two independent layers so a phone does not have to stay open. **Background monitoring** is a scheduled worker that polls Front Desk every fifteen minutes and raises a per-severity notification when a member's health changes, which works everywhere without any push infrastructure. **Real-time push** adds low-latency delivery on top: when Front Desk raises an alert it pushes a wake to the phone over UnifiedPush and ntfy, Bellhop runs an immediate check, and you get the notification within seconds rather than at the next poll. Push is fully optional and self-hosted-friendly, so you can run Bellhop with no Google services at all and still get timely alerts.

## Enabling real-time push

Real-time push uses [UnifiedPush](https://unifiedpush.org) with [ntfy](https://ntfy.sh) as the transport, so there is nothing to type into Bellhop and no Google dependency. You install a distributor app, flip one switch, and hand the topic Bellhop generates to Front Desk.

1. **Install a UnifiedPush distributor.** Install the [ntfy Android app](https://ntfy.sh) from F-Droid or Play. It holds the persistent connection and receives pushes; Bellhop has no push transport of its own. If you self-host ntfy, point the ntfy app at your server in its own settings. Without a distributor installed, Bellhop's push section stays idle and tells you to add one.
2. **Turn on Real-time push.** In Bellhop **Settings → Real-time push**, flip the switch. Bellhop registers with the distributor and, a moment later, shows a **Push topic for Front Desk** with a copy button. (If Android notifications are off for Bellhop, grant them so pushes can arrive.)
3. **Point Front Desk at that topic.** In Front Desk → **Settings → Alerts**, press **Set up alerts** (or **Add destination** if alerts are already configured), pick the **Bellhop** tile, and paste the whole **Push topic for Front Desk** string into the one box. Front Desk splits it into server and topic, composes the Apprise URL, and the next step sends a test through it. Nothing is saved until **Finish**. This needs an `apprise-api` container in the Front Desk stack; [[Alerting]] has the one-line compose addition and the full pipeline.

Once wired, any Front Desk alert wakes Bellhop within seconds. The push carries no data: Bellhop treats it as a wake, runs a fresh fleet poll, and notifies from current Front Desk truth, so it never becomes a second, stale alert source. The one payload it does read is Front Desk's test marker, which a healthy fleet would otherwise leave invisible: a **Send test** shows a "push test received" notification on the phone, while real alerts still notify only on a member going down or recovering, or auto-sync drifting. If you only want plain ntfy notifications without Bellhop's fleet view, skip steps 1 and 2, pick the **Phone (ntfy app)** tile instead, let Front Desk generate the topic, and subscribe to it directly in the ntfy app.

## Privacy and security

Bellhop inherits Model Hotel's privacy posture and adds device-level protection. It never contacts Model Hotel members directly, so it holds no provider API keys and no member admin tokens; the only secret on the phone is its own device token, stored encrypted with the Android Keystore and never shown after pairing. Every operator action is gated behind a biometric or device-PIN prompt, and the whole app can be locked the same way. Front Desk enforces the device's role on the server side, so a Monitor token cannot be tricked into an operator action, and any device can be revoked instantly from either the phone (Unlink) or the Front Desk panel. As with the gateway, Bellhop moves only routing and metering metadata, never the content of requests or responses.

## Unlinking

<p align="center">
<a href="screenshots/bellhop_unlink_confirm.png"><img src="screenshots/bellhop_unlink_confirm.png" width="240" alt="Bellhop unlink confirmation dialog"></a>
</p>

Unlink from **Settings**, at the bottom. Bellhop confirms first, then clears its local token and asks Front Desk to revoke it, so the device stops monitoring and can no longer act on the fleet. You can pair the same phone again anytime with a fresh code. An operator can also revoke the device from the Front Desk **Paired devices** panel without touching the phone, which is the path to use for a lost or stolen device.

## Building and installing

Bellhop lives in [`android/`](https://github.com/hugalafutro/model-hotel/tree/master/android). It is a Kotlin and Jetpack Compose app targeting Android 8.0 (API 26) and up. Build a debug APK with `./gradlew assembleDebug` from `android/`, or a signed release with `./gradlew assembleRelease`. See the [`android/` README](https://github.com/hugalafutro/model-hotel/blob/master/android/README.md) for the current build and signing steps.
