import { renderHook, waitFor } from "@testing-library/react";
import QRCode from "qrcode";
import { expect, it, vi } from "vitest";
import { useQrDataUrl } from "../useQrDataUrl";

it("encodes the text and re-encodes when it changes, never showing the old image", async () => {
	const spy = vi
		.spyOn(QRCode, "toDataURL")
		.mockImplementation((text) => Promise.resolve(`data:${String(text)}`));

	const { result, rerender } = renderHook(
		({ text }) => useQrDataUrl(text, 220),
		{
			initialProps: { text: "first" },
		},
	);
	await waitFor(() => expect(result.current).toBe("data:first"));
	expect(spy).toHaveBeenCalledWith("first", { width: 220, margin: 2 });

	// A second code must not be shown beside the first code's QR while its own
	// encode is still running.
	rerender({ text: "second" });
	expect(result.current).toBe("");
	await waitFor(() => expect(result.current).toBe("data:second"));

	spy.mockRestore();
});

it("renders nothing for an empty value or a failed encode", async () => {
	const spy = vi
		.spyOn(QRCode, "toDataURL")
		.mockRejectedValue(new Error("too much data"));

	const { result, rerender } = renderHook(
		({ text }) => useQrDataUrl(text, 200),
		{
			initialProps: { text: "" },
		},
	);
	expect(result.current).toBe("");
	expect(spy).not.toHaveBeenCalled();

	rerender({ text: "unencodable" });
	await waitFor(() => expect(spy).toHaveBeenCalled());
	expect(result.current).toBe("");

	spy.mockRestore();
});
