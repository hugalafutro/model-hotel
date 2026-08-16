import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { expect, it, vi } from "vitest";
import type { AlertEventDef } from "../../../api/types";
import { ToastProvider } from "../../../context/ToastContext";
import { server } from "../../../test/server";
import { AlertsWizard, type AlertsWizardProps } from "../AlertsWizard";

const catalog: AlertEventDef[] = [
	{
		type: "health.down",
		category: "Health",
		severity: "error",
		defaultOn: true,
	},
	{
		type: "health.up",
		category: "Health",
		severity: "success",
		defaultOn: true,
	},
	{
		type: "member.added",
		category: "Membership",
		severity: "info",
		defaultOn: false,
	},
];

// The ntfy server field runs a soft reachability check on blur. It is a plain
// browser fetch to the operator's own server, so msw (which errors on
// unhandled requests) needs it declared; the result never gates anything.
const softCheck = http.get(
	/\/v1\/health$/,
	() => new HttpResponse(null, { status: 200 }),
);

function renderWizard(over: Partial<AlertsWizardProps> = {}) {
	const onFinished = vi.fn();
	const onClose = vi.fn();
	render(
		<ToastProvider>
			<AlertsWizard
				initialApiUrl=""
				savedTargets={[]}
				savedEvents=""
				catalog={catalog}
				startAt={1}
				onClose={onClose}
				onFinished={onFinished}
				{...over}
			/>
		</ToastProvider>,
	);
	return { onFinished, onClose };
}

it("step 1 gates Next on a healthy probe of the typed URL", async () => {
	let probed = "";
	server.use(
		http.post("/api/alert/probe", async ({ request }) => {
			const b = (await request.json()) as { api_url: string };
			probed = b.api_url;
			return HttpResponse.json(
				b.api_url.includes("good")
					? { configured: true, reachable: true, healthy: true }
					: {
							configured: true,
							reachable: false,
							healthy: false,
							reason: "unreachable",
						},
			);
		}),
	);
	renderWizard();
	expect(screen.getByTestId("wiz-api-url")).toHaveValue("http://apprise:8000");
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.clear(screen.getByTestId("wiz-api-url"));
	await userEvent.type(screen.getByTestId("wiz-api-url"), "http://bad:8000");
	await userEvent.click(screen.getByTestId("wiz-api-check"));
	await waitFor(() =>
		expect(screen.getByTestId("wiz-api-status")).toHaveAttribute(
			"data-ok",
			"false",
		),
	);
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.clear(screen.getByTestId("wiz-api-url"));
	await userEvent.type(screen.getByTestId("wiz-api-url"), "http://good:8000");
	await userEvent.click(screen.getByTestId("wiz-api-check"));
	await waitFor(() =>
		expect(screen.getByTestId("wiz-api-status")).toHaveAttribute(
			"data-ok",
			"true",
		),
	);
	expect(probed).toBe("http://good:8000");
	expect(screen.getByTestId("wiz-next")).toBeEnabled();
	// editing after a green probe re-locks
	await userEvent.type(screen.getByTestId("wiz-api-url"), "1");
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
});

it("steps 2-4: composes an ntfy URL, requires a passing test, and never writes settings", async () => {
	const puts: unknown[] = [];
	const tests: unknown[] = [];
	server.use(
		softCheck,
		http.post("/api/alert/probe", () =>
			HttpResponse.json({ configured: true, reachable: true, healthy: true }),
		),
		http.put("/api/settings", async ({ request }) => {
			puts.push(await request.json());
			return HttpResponse.json({});
		}),
		http.post("/api/alert/test", async ({ request }) => {
			const b = await request.json();
			tests.push(b);
			return String((b as { targets: string[] }).targets[0]).includes("bad")
				? HttpResponse.json(
						{ code: "deliver_failed", error: "x" },
						{ status: 502 },
					)
				: new HttpResponse(null, { status: 204 });
		}),
	);
	renderWizard();
	await userEvent.click(screen.getByTestId("wiz-api-check"));
	await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.click(screen.getByTestId("wiz-kind-ntfy"));
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 3
	expect(screen.getByTestId("wiz-field-server")).toHaveValue(""); // no ntfy.sh prefill
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.type(
		screen.getByTestId("wiz-field-server"),
		"https://bad.example.com",
	);
	await userEvent.click(screen.getByTestId("wiz-generate-topic"));
	const topic = (screen.getByTestId("wiz-field-topic") as HTMLInputElement)
		.value;
	expect(topic).toMatch(/^[A-Za-z0-9]{20}$/);
	expect(screen.getByTestId("wiz-composed")).toHaveTextContent(
		`ntfys://bad.example.com/${topic}`,
	);
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 4
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.click(screen.getByTestId("wiz-send-test"));
	await waitFor(() =>
		expect(screen.getByTestId("wiz-test-result")).toHaveAttribute(
			"data-ok",
			"false",
		),
	);
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.click(screen.getByTestId("wiz-back")); // -> 3, fix
	await userEvent.clear(screen.getByTestId("wiz-field-server"));
	await userEvent.type(
		screen.getByTestId("wiz-field-server"),
		"https://ntfy.example.com",
	);
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 4, tested reset
	await userEvent.click(screen.getByTestId("wiz-send-test"));
	await waitFor(() =>
		expect(screen.getByTestId("wiz-test-result")).toHaveAttribute(
			"data-ok",
			"true",
		),
	);
	expect(tests.at(-1)).toEqual({
		api_url: "http://apprise:8000",
		targets: [`ntfys://ntfy.example.com/${topic}`],
	});
	expect(screen.getByTestId("wiz-next")).toBeEnabled();
	// a passing test describes one URL only: editing the destination re-locks it
	await userEvent.click(screen.getByTestId("wiz-back")); // -> 3
	await userEvent.type(screen.getByTestId("wiz-field-topic"), "x");
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 4
	expect(screen.queryByTestId("wiz-test-result")).toBeNull();
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	expect(puts).toHaveLength(0);
});

it("bellhop tile parses a pasted endpoint and rejects junk", async () => {
	server.use(
		http.post("/api/alert/probe", () =>
			HttpResponse.json({ configured: true, reachable: true, healthy: true }),
		),
	);
	renderWizard({ initialApiUrl: "http://apprise:8000", startAt: 2 });
	await userEvent.click(screen.getByTestId("wiz-kind-bellhop"));
	await userEvent.click(screen.getByTestId("wiz-next"));
	await userEvent.type(screen.getByTestId("wiz-field-endpoint"), "nope");
	expect(screen.getByTestId("wiz-bellhop-error")).toBeInTheDocument();
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	await userEvent.clear(screen.getByTestId("wiz-field-endpoint"));
	await userEvent.type(
		screen.getByTestId("wiz-field-endpoint"),
		"https://ntfy.example.com/upAbCdEfGh?up=1",
	);
	expect(screen.getByTestId("wiz-bellhop-parsed")).toHaveTextContent(
		"upAbCdEfGh",
	);
	expect(screen.getByTestId("wiz-composed")).toHaveTextContent(
		"ntfys://ntfy.example.com/upAbCdEfGh",
	);
	expect(screen.getByTestId("wiz-next")).toBeEnabled();
});

it("falls back to step 1 when the saved apprise URL no longer answers", async () => {
	server.use(
		http.post("/api/alert/probe", () =>
			HttpResponse.json({
				configured: true,
				reachable: false,
				healthy: false,
				reason: "unreachable",
			}),
		),
	);
	renderWizard({ initialApiUrl: "http://apprise:8000", startAt: 2 });
	await waitFor(() =>
		expect(screen.getByTestId("wiz-api-status")).toHaveAttribute(
			"data-ok",
			"false",
		),
	);
	expect(screen.getByTestId("wiz-api-url")).toHaveValue("http://apprise:8000");
	expect(screen.getByTestId("wiz-next")).toBeDisabled();
	// step 1 is now the first reachable step, so there is nothing to go back to
	expect(screen.queryByTestId("wiz-back")).toBeNull();
});

it("cancel closes without any request", async () => {
	const puts: unknown[] = [];
	server.use(
		http.put("/api/settings", async () => {
			puts.push(1);
			return HttpResponse.json({});
		}),
	);
	const { onClose } = renderWizard();
	await userEvent.click(screen.getByTestId("wiz-cancel"));
	expect(onClose).toHaveBeenCalled();
	expect(puts).toHaveLength(0);
});
