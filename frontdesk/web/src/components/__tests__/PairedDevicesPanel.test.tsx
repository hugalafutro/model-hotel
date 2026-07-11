import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it, vi } from "vitest";
import type { PairedDevice, PairStart } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { PairedDevicesPanel } from "../PairedDevicesPanel";

const device: PairedDevice = {
	id: "dev-1",
	label: "Pixel 8",
	role: "operator",
	created_at: "2026-07-10T12:00:00Z",
	last_seen_at: "2026-07-10T12:30:00Z",
};

const monitorDevice: PairedDevice = {
	id: "dev-2",
	label: "Kitchen tablet",
	role: "monitor",
	created_at: "2026-07-09T09:00:00Z",
};

const pairStart: PairStart = {
	code: "PAIRCODE234567",
	role: "operator",
	// Far-future expiry so the code never flips to expired mid-test.
	expires_at: "2099-01-01T00:00:00Z",
};

function handlers(devices: PairedDevice[]) {
	return [
		http.get("/api/devices", () => HttpResponse.json(devices)),
		http.post("/api/pair/start", () => HttpResponse.json(pairStart)),
	];
}

function renderPanel() {
	render(
		<ToastProvider>
			<PairedDevicesPanel />
		</ToastProvider>,
	);
}

beforeEach(() => {
	server.resetHandlers();
});

it("lists paired devices with role badges and last-seen", async () => {
	server.use(...handlers([device, monitorDevice]));
	renderPanel();

	expect(await screen.findByText("Pixel 8")).toBeInTheDocument();
	expect(screen.getByText("Kitchen tablet")).toBeInTheDocument();

	const rows = screen.getAllByRole("row").slice(1); // skip header
	expect(within(rows[0]).getByText("Operator")).toHaveAttribute(
		"data-test-variant",
		"operator",
	);
	expect(within(rows[1]).getByText("Monitor")).toHaveAttribute(
		"data-test-variant",
		"monitor",
	);
	// A device that never authenticated shows the never-seen placeholder.
	expect(within(rows[1]).getByText("Never")).toBeInTheDocument();
});

it("shows the empty state when nothing is paired", async () => {
	server.use(...handlers([]));
	renderPanel();
	expect(await screen.findByText("No devices paired yet.")).toBeInTheDocument();
});

it("stays quiet when the device list cannot be fetched", async () => {
	// Like the other Settings panels, a failed initial load renders nothing so
	// the rest of the page still works.
	server.use(
		http.get("/api/devices", () => new HttpResponse(null, { status: 500 })),
	);
	renderPanel();
	await waitFor(() => {
		expect(screen.queryByText("Paired devices")).not.toBeInTheDocument();
	});
});

it("mints a pairing code and renders QR plus copyable pairing string", async () => {
	server.use(...handlers([]));
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));

	// The pairing string carries the full payload: origin, code, and host.
	const stringField = await screen.findByLabelText("Pairing string");
	const payload = JSON.parse((stringField as HTMLTextAreaElement).value);
	expect(payload.pairing_code).toBe(pairStart.code);
	expect(payload.fd_url).toBe(window.location.origin);
	expect(payload.fd_name).toBe(window.location.host);

	// The QR image renders from the same payload (data URL from the qrcode lib).
	await waitFor(() => {
		const img = screen.getByRole("img", { name: "Pairing QR code" });
		expect(img.getAttribute("src")).toMatch(/^data:image\/png/);
	});

	// The button relabels for regeneration.
	expect(screen.getByRole("button", { name: "New code" })).toBeInTheDocument();
});

it("dismisses the pairing code once a device pairs with it", async () => {
	// Empty until the phone pairs; then the poll picks up the new device AND the
	// code stops being outstanding.
	let paired = false;
	server.use(
		http.get("/api/devices", () => HttpResponse.json(paired ? [device] : [])),
		http.post("/api/pair/start", () => HttpResponse.json(pairStart)),
		http.post("/api/pair/status", () =>
			HttpResponse.json({ outstanding: !paired }),
		),
	);
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	await screen.findByLabelText("Pairing string");

	// Phone redeems the code: the device appears and the code is now spent, so
	// the QR/string block dismisses itself.
	paired = true;
	await waitFor(
		() => {
			expect(screen.queryByLabelText("Pairing string")).not.toBeInTheDocument();
		},
		{ timeout: 7000 },
	);
	expect(screen.getByText("Pixel 8")).toBeInTheDocument();
	expect(
		screen.queryByRole("img", { name: "Pairing QR code" }),
	).not.toBeInTheDocument();
	// Back to the idle "Pair device" button.
	expect(
		screen.getByRole("button", { name: "Pair device" }),
	).toBeInTheDocument();
}, 10000);

it("keeps the code when another operator's device pairs concurrently", async () => {
	// A different device appears after our code is shown, but OUR code stays
	// outstanding: the string must not be stolen.
	let others: PairedDevice[] = [];
	server.use(
		http.get("/api/devices", () => HttpResponse.json(others)),
		http.post("/api/pair/start", () => HttpResponse.json(pairStart)),
		http.post("/api/pair/status", () =>
			HttpResponse.json({ outstanding: true }),
		),
	);
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	await screen.findByLabelText("Pairing string");

	// Someone else pairs their device; the next poll surfaces it.
	others = [monitorDevice];
	await screen.findByText("Kitchen tablet", undefined, { timeout: 7000 });

	// Our code is still outstanding, so the pairing string stays.
	expect(screen.getByLabelText("Pairing string")).toBeInTheDocument();
}, 10000);

it("copies the pairing string to the clipboard", async () => {
	server.use(...handlers([]));
	const writeText = vi.fn().mockResolvedValue(undefined);
	Object.assign(navigator, { clipboard: { writeText } });
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	await screen.findByLabelText("Pairing string");
	await userEvent.click(
		screen.getByRole("button", { name: "Copy pairing string" }),
	);
	expect(writeText).toHaveBeenCalledWith(
		expect.stringContaining(pairStart.code),
	);
});

it("shows the expired notice when the code TTL has passed", async () => {
	server.use(
		http.get("/api/devices", () => HttpResponse.json([])),
		http.post("/api/pair/start", () =>
			HttpResponse.json({ ...pairStart, expires_at: "2000-01-01T00:00:00Z" }),
		),
	);
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	expect(await screen.findByRole("alert")).toHaveTextContent(
		"This pairing code expired.",
	);
});

it("revokes a device through the confirm modal", async () => {
	let revoked = false;
	server.use(
		http.get("/api/devices", () => HttpResponse.json(revoked ? [] : [device])),
		http.delete("/api/devices/dev-1", () => {
			revoked = true;
			return HttpResponse.json({ success: true });
		}),
	);
	renderPanel();
	await screen.findByText("Pixel 8");

	await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
	const dialog = await screen.findByRole("dialog");
	expect(dialog).toHaveTextContent("Pixel 8");
	await userEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

	await waitFor(() => {
		expect(screen.getByText("No devices paired yet.")).toBeInTheDocument();
	});
	expect(revoked).toBe(true);
});

it("surfaces a toast when minting a code fails", async () => {
	server.use(
		http.get("/api/devices", () => HttpResponse.json([])),
		http.post("/api/pair/start", () => new HttpResponse(null, { status: 500 })),
	);
	renderPanel();
	await screen.findByText("No devices paired yet.");

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
	// No pairing string appears on failure.
	expect(screen.queryByLabelText("Pairing string")).not.toBeInTheDocument();
});

it("switches role: hint text updates and the monitor role is requested", async () => {
	let requestedRole = "";
	server.use(
		http.get("/api/devices", () => HttpResponse.json([])),
		http.post("/api/pair/start", async ({ request }) => {
			requestedRole = ((await request.json()) as { role: string }).role;
			return HttpResponse.json({ ...pairStart, role: "monitor" });
		}),
	);
	renderPanel();
	await screen.findByText("No devices paired yet.");

	expect(screen.getByText(/Operator devices can also/)).toBeInTheDocument();
	await userEvent.selectOptions(screen.getByLabelText("Role"), "monitor");
	expect(screen.getByText(/Monitor devices are read-only/)).toBeInTheDocument();

	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	await screen.findByLabelText("Pairing string");
	expect(requestedRole).toBe("monitor");
});

it("selects the pairing string on focus for easy manual copying", async () => {
	server.use(...handlers([]));
	renderPanel();
	await screen.findByText("No devices paired yet.");
	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));

	const field = (await screen.findByLabelText(
		"Pairing string",
	)) as HTMLTextAreaElement;
	await userEvent.click(field);
	expect(field.selectionEnd - field.selectionStart).toBe(field.value.length);
});

it("shows an error toast when the clipboard write fails", async () => {
	server.use(...handlers([]));
	Object.assign(navigator, {
		clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
	});
	renderPanel();
	await screen.findByText("No devices paired yet.");
	await userEvent.click(screen.getByRole("button", { name: "Pair device" }));
	await screen.findByLabelText("Pairing string");
	await userEvent.click(
		screen.getByRole("button", { name: "Copy pairing string" }),
	);
	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
});

it("keeps the device and shows an error toast when revoking fails", async () => {
	server.use(
		http.get("/api/devices", () => HttpResponse.json([device])),
		http.delete(
			"/api/devices/dev-1",
			() => new HttpResponse(null, { status: 500 }),
		),
	);
	renderPanel();
	await screen.findByText("Pixel 8");

	await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
	const dialog = await screen.findByRole("dialog");
	await userEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
	// The modal stays open (the action failed) and the device is still listed.
	expect(screen.getByRole("dialog")).toBeInTheDocument();
	expect(screen.getByText("Pixel 8")).toBeInTheDocument();
});

it("closes the revoke modal on cancel without revoking", async () => {
	server.use(...handlers([device]));
	renderPanel();
	await screen.findByText("Pixel 8");

	await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
	const dialog = await screen.findByRole("dialog");
	await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
	expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	expect(screen.getByText("Pixel 8")).toBeInTheDocument();
});
