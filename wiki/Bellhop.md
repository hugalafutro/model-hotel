# Bellhop (Android Companion App)

Bellhop is the Android companion app for [[High Availability|Front Desk]]: pair a phone once and it becomes a pocket view of the fleet. It talks only to Front Desk (never to Model Hotel members directly), holds no member credentials, and authenticates with its own device token that can be revoked from either side at any time. The app is in early development: pairing, the linked/unlinked gate, and unlink work today; the read-only dashboard with live member health, events, and alerts is the next slice, followed by notifications and operator actions (drain, activate, config sync) behind a biometric prompt.

## Linking a device

Linking starts on the Front Desk side. Open Settings, then Paired devices, pick the device's role (Monitor is read-only; Operator can also drain or activate members, trigger a config sync, and toggle auto-sync) and generate a pairing code. The code renders as a QR and as a copyable pairing string carrying the same payload: the Front Desk URL, the one-time code, and a display name. Codes are single-use and expire after a few minutes, and the panel dismisses the code on its own once your device pairs with it.

<p align="center"><a href="screenshots/frontdesk_settings_devices_pairing.png"><img src="screenshots/frontdesk_settings_devices_pairing.png" width="800" alt="Front Desk Settings, Paired devices: pairing QR and copyable pairing string"></a></p>

On the phone, paste the pairing string (QR scanning is coming soon), check that the shown Front Desk is the one you meant, optionally rename the device, and tap Pair. Bellhop exchanges the one-time code for its device token, which Front Desk returns exactly once and Bellhop stores encrypted at rest (Android Keystore, AES-GCM); the token itself is never displayed anywhere again. Every paired device stays listed in the panel with its role, pairing time, and last-seen time. Revoke there to remotely unlink a lost phone, or unlink from the app itself, which revokes the token on Front Desk and wipes all local state.

<p align="center"><a href="screenshots/frontdesk_settings_devices.png"><img src="screenshots/frontdesk_settings_devices.png" width="800" alt="Front Desk Settings, Paired devices: one linked device with role and last-seen time"></a></p>

## App flow

The current build's linking flow, start to finish (tap any screen for full size):

<p align="center">
  <a href="screenshots/bellhop_pairing.png"><img src="screenshots/bellhop_pairing.png" width="200" alt="Bellhop: link a Front Desk"></a>
  <a href="screenshots/bellhop_pairing_filled.png"><img src="screenshots/bellhop_pairing_filled.png" width="200" alt="Bellhop: pairing string pasted and parsed"></a>
  <a href="screenshots/bellhop_dashboard.png"><img src="screenshots/bellhop_dashboard.png" width="200" alt="Bellhop: linked home screen"></a>
  <a href="screenshots/bellhop_unlink_confirm.png"><img src="screenshots/bellhop_unlink_confirm.png" width="200" alt="Bellhop: unlink confirmation dialog"></a>
</p>
