import QRCode from "qrcode";
import { useEffect, useState } from "react";

// useQrDataUrl renders a string as a QR code data URL for an <img>. Pairing and
// TOTP enrolment both offer a scan-or-type pair, so the same encode-and-forget
// effect belongs in one place: an empty value renders nothing, and a failed
// encode leaves an empty string rather than a broken image.
export function useQrDataUrl(text: string, width: number): string {
	// The encoded image is kept with the value it was encoded from, so a stale QR
	// is never shown beside a code it does not encode. Storing the pair (rather
	// than clearing the URL when the value changes) keeps the effect free of a
	// synchronous setState and the cascading render that comes with it.
	const [qr, setQr] = useState({ text: "", url: "" });

	useEffect(() => {
		if (!text) return;
		let cancelled = false;
		QRCode.toDataURL(text, { width, margin: 2 })
			.then((url) => {
				if (!cancelled) setQr({ text, url });
			})
			.catch(() => {
				if (!cancelled) setQr({ text, url: "" });
			});
		return () => {
			cancelled = true;
		};
	}, [text, width]);

	return qr.text === text ? qr.url : "";
}
