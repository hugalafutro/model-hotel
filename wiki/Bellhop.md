# Bellhop (Android Companion App)

Bellhop is the Android companion app for [[High Availability|Front Desk]]. Pair a phone once and it becomes a pocket view of your fleet: live member health, request traffic, the fleet event log, and, for operator devices, the controls to drain a member, activate it again, or push a config sync, all from a lock screen notification tap. Bellhop talks only to Front Desk, never to Model Hotel members directly, so it holds no provider credentials and no member admin tokens. It authenticates with its own device token that either side can revoke at any time, and like the rest of Model Hotel it moves only routing and metering metadata, never prompt or response content.

## The app at a glance

<p align="center">
<a href="screenshots/bellhop_dashboard.png"><img src="screenshots/bellhop_dashboard.png" width="240" alt="Bellhop dashboard: linked Front Desk, auto-sync, two healthy members with traffic sparklines"></a>
<a href="screenshots/bellhop_member.png"><img src="screenshots/bellhop_member.png" width="240" alt="Bellhop member detail: traffic graph, operator controls, recent events"></a>
<a href="screenshots/bellhop_events.png"><img src="screenshots/bellhop_events.png" width="240" alt="Bellhop fleet event log with date-range filter"></a>
</p>

Three surfaces cover day-to-day monitoring. The **dashboard** is the linked home screen: it names the Front Desk you paired with, carries an auto-sync switch for operators, shows a fleet health banner, and lists every member as a card with its health dot, address, latency, Traefik status, version, a request sparkline, and its latest event. Tapping a card opens the **member detail** screen with a full request-traffic graph, the member's metadata, operator controls, and its recent events. The top-bar log icon opens the fleet-wide **events** screen. Tap any screenshot on this page to view it full size.

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

The dashboard is the linked home screen shown at the top of this page. A banner summarizes fleet health at a glance ("All members up", or a count when something is down), and an auto-sync switch (operator only) reflects and controls whether members track the primary's config. Each member card carries a colored health dot, the member name and a **Primary** badge where it applies, the member address, a compact status line (reachability, latency, Traefik state, and running version), a request-traffic sparkline drawn only when the card is on screen to save battery, and the member's most recent event with a relative timestamp. Pull to refresh forces an immediate poll.

## Member detail

<p align="center">
<a href="screenshots/bellhop_member.png"><img src="screenshots/bellhop_member.png" width="260" alt="Bellhop member detail: traffic graph at last 3 hours, operator controls, recent events"></a>
</p>

Tapping a card opens the member. The top of the screen is a **request-traffic graph** over the window you chose in Settings (requests and errors are drawn as separate series with their own legend, and the time axis spans the selected range). Below it sit the member's address, running version, and when it was added to the fleet. The **Operator controls** section (present only on operator devices) offers **Drain** to bleed traffic off a member, **Activate** to bring a drained member back, and **Sync fleet config** to push the primary's config out; each action asks for a biometric confirmation and reports back whether Front Desk accepted it. A **Recent events** list closes the screen, with its own date-range chips so you can narrow it to the last hour or open it up to all time.

## Events

The events screen is the fleet-wide log. A header counts the events in view, and a row of range chips (1h, 24h, 7d, 30d, All) plus a calendar picker scope the list. Each entry shows a human title and one-line summary, a severity (Error, Warning, Info, or Success) shown as a colored edge, and the raw event type with its source and member (for example `health.down` from `frontdesk-poller` on a given member). The log covers member health transitions, version read failures and recoveries, config syncs (manual and automatic), and device pairing and revocation, so it doubles as an audit trail of everything Front Desk noticed.

## Alerts

<p align="center">
<a href="screenshots/bellhop_settings_alerts.png"><img src="screenshots/bellhop_settings_alerts.png" width="240" alt="Bellhop settings: Alerts card with per-severity badge counts"></a>
<a href="screenshots/bellhop_alerts.png"><img src="screenshots/bellhop_alerts.png" width="240" alt="Bellhop alerts: What Front Desk alerts on, with per-event toggles"></a>
</p>

The **Alerts** screen shows what Front Desk raises alerts for and, on operator devices, lets you change it. Events are grouped (Health, Config Sync, and so on), each with a severity badge and a switch; flipping a switch enables or mutes that alert on Front Desk right away, so the phone acts as a remote control for the fleet's alerting policy rather than just a viewer of it. A **Notification delivery** panel at the top reports whether an outbound channel (such as an Apprise target) is configured on Front Desk. Monitor devices see the same screen read-only.

## Settings

<p align="center">
<a href="screenshots/bellhop_settings.png"><img src="screenshots/bellhop_settings.png" width="240" alt="Bellhop settings: linked Front Desk, app lock, background monitoring, real-time push, traffic graph range"></a>
<a href="screenshots/bellhop_language.png"><img src="screenshots/bellhop_language.png" width="240" alt="Bellhop language picker with system default and ten locales"></a>
</p>

Settings gathers the device-side preferences. **Linked Front Desk** shows which Front Desk you paired with, the name and role you linked as, and the date, and it long-presses to copy. **App lock** requires a fingerprint or device PIN to open Bellhop at all. **Background monitoring** checks the fleet every fifteen minutes and notifies you when a member goes down or recovers, even while the app is closed. **Real-time push** wakes Bellhop the instant Front Desk pushes an alert, over UnifiedPush and ntfy, with no Google dependency and no polling delay; it is opt-in. **Traffic graph range** sets how far back the request charts reach (1h, 3h, 6h, 12h, or 24h). **Hold to copy** toggles whether long-pressing an event or member cell copies it to the clipboard. **Language** offers the system default plus ten hand-translated locales. The screen ends with **Unlink**.

## Notifications and background monitoring

Bellhop keeps you informed with two independent layers so a phone does not have to stay open. **Background monitoring** is a scheduled worker that polls Front Desk every fifteen minutes and raises a per-severity notification when a member's health changes, which works everywhere without any push infrastructure. **Real-time push** adds low-latency delivery on top: when Front Desk raises an alert it pushes a wake to the phone over UnifiedPush and ntfy, Bellhop runs an immediate check, and you get the notification within seconds rather than at the next poll. Push is fully optional and self-hosted-friendly, so you can run Bellhop with no Google services at all and still get timely alerts.

## Privacy and security

Bellhop inherits Model Hotel's privacy posture and adds device-level protection. It never contacts Model Hotel members directly, so it holds no provider API keys and no member admin tokens; the only secret on the phone is its own device token, stored encrypted with the Android Keystore and never shown after pairing. Every operator action is gated behind a biometric or device-PIN prompt, and the whole app can be locked the same way. Front Desk enforces the device's role on the server side, so a Monitor token cannot be tricked into an operator action, and any device can be revoked instantly from either the phone (Unlink) or the Front Desk panel. As with the gateway, Bellhop moves only routing and metering metadata, never the content of requests or responses.

## Unlinking

<p align="center">
<a href="screenshots/bellhop_unlink_confirm.png"><img src="screenshots/bellhop_unlink_confirm.png" width="240" alt="Bellhop unlink confirmation dialog"></a>
</p>

Unlink from **Settings**, at the bottom. Bellhop confirms first, then clears its local token and asks Front Desk to revoke it, so the device stops monitoring and can no longer act on the fleet. You can pair the same phone again anytime with a fresh code. An operator can also revoke the device from the Front Desk **Paired devices** panel without touching the phone, which is the path to use for a lost or stolen device.

## Building and installing

Bellhop lives in [`android/`](https://github.com/hugalafutro/model-hotel/tree/master/android). It is a Kotlin and Jetpack Compose app targeting Android 8.0 (API 26) and up. Build a debug APK with `./gradlew assembleDebug` from `android/`, or a signed release with `./gradlew assembleRelease`. See the [`android/` README](https://github.com/hugalafutro/model-hotel/blob/master/android/README.md) for the current build and signing steps.
