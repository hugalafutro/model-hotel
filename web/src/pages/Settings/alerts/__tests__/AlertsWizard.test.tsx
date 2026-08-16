import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import type { AlertEventDef } from "../../../../api/types";
import i18n from "../../../../i18n";
import { server } from "../../../../test/mocks/server";
import { renderWithProviders } from "../../../../test/utils";
import {
	AlertsWizard,
	type AlertsWizardProps,
	initialState,
	isDuplicate,
	reducer,
} from "../AlertsWizard";

const catalog: AlertEventDef[] = [
	{
		type: "circuit_breaker_open",
		category: "Health",
		severity: "error",
		defaultOn: true,
	},
	{
		type: "circuit_breaker_closed",
		category: "Health",
		severity: "success",
		defaultOn: true,
	},
	{
		type: "fleet_conflict",
		category: "Fleet",
		severity: "info",
		defaultOn: false,
	},
];

function props(over: Partial<AlertsWizardProps> = {}): AlertsWizardProps {
	return {
		initialApiUrl: "",
		savedTargets: [],
		savedEvents: null,
		catalog,
		startAt: 1,
		onClose: () => {},
		onFinished: () => {},
		...over,
	};
}

function renderWizard(over: Partial<AlertsWizardProps> = {}) {
	const onFinished = vi.fn();
	const onClose = vi.fn();
	renderWithProviders(
		<AlertsWizard {...props({ onClose, onFinished, ...over })} />,
	);
	return { onFinished, onClose };
}

/** A probe that reports a healthy apprise-api whatever it is pointed at. */
function healthyProbe() {
	server.use(
		http.post("/api/alert/probe", () =>
			HttpResponse.json({ configured: true, reachable: true, healthy: true }),
		),
	);
}

/** A test endpoint that delivers whatever it is handed. */
function passingTest() {
	server.use(
		http.post("/api/alert/test", () => HttpResponse.json({ ok: true })),
	);
}

// addNtfy drives steps 2-4 for one ntfy destination (kind -> details -> passing
// test) and clicks Next onto step 5. Callers mock POST /api/alert/test.
async function addNtfy(serverUrl: string, topic: string) {
	await userEvent.click(screen.getByTestId("wiz-kind-ntfy"));
	await userEvent.click(screen.getByTestId("wiz-next"));
	await userEvent.type(screen.getByTestId("wiz-field-server"), serverUrl);
	await userEvent.type(screen.getByTestId("wiz-field-topic"), topic);
	await userEvent.click(screen.getByTestId("wiz-next"));
	await userEvent.click(screen.getByTestId("wiz-send-test"));
	await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
	await userEvent.click(screen.getByTestId("wiz-next"));
}

describe("AlertsWizard", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

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
		expect(screen.getByTestId("wiz-api-url")).toHaveValue(
			"http://apprise:8000",
		);
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
		// The failure is explained by the server's own reason code.
		expect(screen.getByTestId("wiz-api-status").textContent).toBe(
			i18n.t("settings.alerts.reason.unreachable"),
		);
		expect(screen.getByTestId("wiz-api-status")).toHaveClass("ui-text-error");
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
		expect(screen.getByTestId("wiz-api-status")).toHaveClass("ui-text-success");
		expect(probed).toBe("http://good:8000");
		expect(screen.getByTestId("wiz-next")).toBeEnabled();
		// editing after a green probe re-locks
		await userEvent.type(screen.getByTestId("wiz-api-url"), "1");
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		expect(screen.queryByTestId("wiz-api-status")).toBeNull();
	});

	it("steps 2-4: composes an ntfy URL, requires a passing test, and never writes settings", async () => {
		const puts: unknown[] = [];
		const tests: unknown[] = [];
		healthyProbe();
		server.use(
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
					: HttpResponse.json({ ok: true });
			}),
		);
		renderWizard();
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		// Model Hotel has no Bellhop app, so the wizard offers no tile for one.
		expect(screen.queryByTestId("wiz-kind-bellhop")).toBeNull();
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
		// the 502's code is resolved through the shared reason catalog
		expect(screen.getByTestId("wiz-test-result").textContent).toBe(
			i18n.t("settings.alerts.reason.deliver_failed"),
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
		expect(screen.getByTestId("wiz-test-result")).toHaveClass(
			"ui-text-success",
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

	it("copies the ntfy server and topic for retyping on a phone", async () => {
		healthyProbe();
		renderWizard({ initialApiUrl: "http://apprise:8000", startAt: 2 });
		// Spied after render: userEvent.setup() installs its own clipboard stub,
		// so the spy has to go on whatever object is in place by then.
		const writeText = vi
			.spyOn(navigator.clipboard, "writeText")
			.mockResolvedValue(undefined);
		await userEvent.click(screen.getByTestId("wiz-kind-ntfy"));
		await userEvent.click(screen.getByTestId("wiz-next"));
		// Nothing is offered to copy until there is something to copy.
		expect(screen.queryByTestId("wiz-copy-topic")).toBeNull();
		await userEvent.type(
			screen.getByTestId("wiz-field-server"),
			"https://ntfy.example.com",
		);
		await userEvent.type(screen.getByTestId("wiz-field-topic"), "abcabcabc");
		await userEvent.click(screen.getByTestId("wiz-copy-server"));
		await userEvent.click(screen.getByTestId("wiz-copy-topic"));
		expect(writeText.mock.calls.flat()).toEqual([
			"https://ntfy.example.com",
			"abcabcabc",
		]);
	});

	it("discord tile says why a webhook that will not parse blocks Next", async () => {
		healthyProbe();
		renderWizard({ initialApiUrl: "http://apprise:8000", startAt: 2 });
		await userEvent.click(screen.getByTestId("wiz-kind-discord"));
		await userEvent.click(screen.getByTestId("wiz-next"));
		await userEvent.type(screen.getByTestId("wiz-field-webhook"), "nope");
		expect(screen.getByTestId("wiz-discord-error")).toBeInTheDocument();
		expect(screen.queryByTestId("wiz-composed")).toBeNull();
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		await userEvent.clear(screen.getByTestId("wiz-field-webhook"));
		await userEvent.type(
			screen.getByTestId("wiz-field-webhook"),
			"https://discord.com/api/webhooks/123456789/AbCdEf",
		);
		expect(screen.queryByTestId("wiz-discord-error")).toBeNull();
		expect(screen.getByTestId("wiz-composed")).toHaveTextContent(
			"discord://123456789/AbCdEf",
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
		expect(screen.getByTestId("wiz-api-url")).toHaveValue(
			"http://apprise:8000",
		);
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		// step 1 is now the first reachable step, so there is nothing to go back to
		expect(screen.queryByTestId("wiz-back")).toBeNull();
	});

	it("re-locks a tested destination when the apprise URL changes", async () => {
		const tests: { api_url: string }[] = [];
		healthyProbe();
		server.use(
			http.post("/api/alert/test", async ({ request }) => {
				tests.push((await request.json()) as { api_url: string });
				return HttpResponse.json({ ok: true });
			}),
		);
		renderWizard();
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
		await userEvent.click(screen.getByTestId("wiz-kind-ntfy"));
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 3
		await userEvent.type(
			screen.getByTestId("wiz-field-server"),
			"https://ntfy.example.com",
		);
		await userEvent.click(screen.getByTestId("wiz-generate-topic"));
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 4
		await userEvent.click(screen.getByTestId("wiz-send-test"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-test-result")).toHaveAttribute(
				"data-ok",
				"true",
			),
		);

		// walk back to step 1 and point at a different apprise
		await userEvent.click(screen.getByTestId("wiz-back")); // -> 3
		await userEvent.click(screen.getByTestId("wiz-back")); // -> 2
		await userEvent.click(screen.getByTestId("wiz-back")); // -> 1
		await userEvent.clear(screen.getByTestId("wiz-api-url"));
		await userEvent.type(
			screen.getByTestId("wiz-api-url"),
			"http://apprise-b:8000",
		);
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-api-status")).toHaveAttribute(
				"data-ok",
				"true",
			),
		);
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 3
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 4
		// the destination was only ever proven through the old apprise
		expect(screen.queryByTestId("wiz-test-result")).toBeNull();
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		await userEvent.click(screen.getByTestId("wiz-send-test"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		expect(tests.at(-1)?.api_url).toBe("http://apprise-b:8000");
	});

	it("refuses a destination that is already stored until it is changed", async () => {
		healthyProbe();
		passingTest();
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			startAt: 2,
		});
		await userEvent.click(screen.getByTestId("wiz-kind-other"));
		await userEvent.click(screen.getByTestId("wiz-next"));

		// A URL nobody has stored is just a new destination.
		await userEvent.type(screen.getByTestId("wiz-field-url"), "tgram://9/9");
		expect(screen.queryByTestId("wiz-already-saved")).toBeNull();
		expect(screen.getByTestId("wiz-next")).toBeEnabled();

		// Typing one that is already stored is a hard stop: the same destination
		// twice is never what was meant, so the step says so and refuses to advance.
		await userEvent.clear(screen.getByTestId("wiz-field-url"));
		await userEvent.type(screen.getByTestId("wiz-field-url"), "tgram://1/2");
		expect(screen.getByTestId("wiz-already-saved")).toHaveTextContent(
			i18n.t("settings.alerts.wizard.alreadySaved"),
		);
		expect(screen.getByTestId("wiz-next")).toBeDisabled();

		// Changing it into a destination the run does not have yet clears both.
		await userEvent.clear(screen.getByTestId("wiz-field-url"));
		await userEvent.type(screen.getByTestId("wiz-field-url"), "tgram://3/4");
		expect(screen.queryByTestId("wiz-already-saved")).toBeNull();
		expect(screen.getByTestId("wiz-next")).toBeEnabled();

		await userEvent.click(screen.getByTestId("wiz-next"));
		await userEvent.click(screen.getByTestId("wiz-send-test"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await userEvent.click(screen.getByTestId("wiz-next"));
		expect(screen.getByTestId("wiz-step-5")).toBeInTheDocument();
	});

	it("warns on step 1 that changing the address drops this run's destinations", async () => {
		healthyProbe();
		passingTest();
		renderWizard();
		// Nothing has been added yet, so there is nothing to warn about.
		expect(screen.queryByTestId("wiz-api-changed-drops")).toBeNull();
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5

		for (let i = 0; i < 4; i += 1) {
			await userEvent.click(screen.getByTestId("wiz-back"));
		}
		expect(screen.getByTestId("wiz-step-1")).toBeInTheDocument();
		expect(screen.getByTestId("wiz-api-changed-drops")).toBeInTheDocument();

		// Editing the address carries out exactly what the note promised.
		await userEvent.type(screen.getByTestId("wiz-api-url"), "1");
		expect(screen.queryByTestId("wiz-api-changed-drops")).toBeNull();
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

	it("a stray backdrop click does not close the wizard or lose the typed input", async () => {
		const { onClose } = renderWizard();
		await userEvent.clear(screen.getByTestId("wiz-api-url"));
		await userEvent.type(
			screen.getByTestId("wiz-api-url"),
			"http://typed:8000",
		);
		await userEvent.click(
			screen.getByRole("button", { name: i18n.t("common.closeDialog") }),
		);
		expect(onClose).not.toHaveBeenCalled();
		expect(screen.getByTestId("wiz-api-url")).toHaveValue("http://typed:8000");
		expect(screen.getByTestId("wiz-step-1")).toBeInTheDocument();
		await userEvent.click(screen.getByTestId("wiz-cancel"));
		expect(onClose).toHaveBeenCalled();
	});

	// Cancel is the "I am not doing this after all" exit, and a probe or a test in
	// flight changes nothing that is stored: it stays live while they run.
	it("keeps Cancel available while a probe is in flight", async () => {
		server.use(
			http.post("/api/alert/probe", async () => {
				await new Promise((r) => setTimeout(r, 40));
				return HttpResponse.json({
					configured: true,
					reachable: true,
					healthy: true,
				});
			}),
		);
		const { onClose } = renderWizard();
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		expect(screen.getByTestId("wiz-api-check")).toBeDisabled();
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		expect(screen.getByTestId("wiz-cancel")).toBeEnabled();
		await userEvent.click(screen.getByTestId("wiz-cancel"));
		expect(onClose).toHaveBeenCalled();
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
	});

	it("accepting an edited destination replaces the one it superseded", () => {
		const ntfyServer = "https://ntfy.example.com";
		const accept = (from: ReturnType<typeof initialState>, topic: string) => {
			let next = reducer(from, {
				type: "setField",
				key: "topic",
				value: topic,
			});
			next = reducer(next, { type: "tested" });
			return reducer(next, { type: "acceptDraft" });
		};

		let s = reducer(initialState(props()), {
			type: "setKind",
			kind: "ntfy",
			ntfyServer,
		});
		s = accept(s, "one");
		expect(s.added).toEqual(["ntfys://ntfy.example.com/one"]);

		// Back to the details, fix the topic, test again, accept again: the wizard
		// carries one destination forward, not the superseded URL beside it.
		s = reducer(s, { type: "go", step: 3 });
		s = accept(s, "two");
		expect(s.added).toEqual(["ntfys://ntfy.example.com/two"]);

		// "Add another" starts a fresh draft, which appends rather than replaces.
		s = reducer(s, { type: "setKind", kind: "ntfy", ntfyServer });
		s = accept(s, "three");
		expect(s.added).toEqual([
			"ntfys://ntfy.example.com/two",
			"ntfys://ntfy.example.com/three",
		]);
	});

	it("does not add a destination that is already stored a second time", () => {
		let s = reducer(
			initialState(
				props({
					initialApiUrl: "http://apprise:8000",
					savedTargets: ["ntfys://ntfy.example.com/one"],
				}),
			),
			{ type: "setKind", kind: "ntfy", ntfyServer: "https://ntfy.example.com" },
		);
		s = reducer(s, { type: "setField", key: "topic", value: "one" });
		s = reducer(s, { type: "tested" });
		s = reducer(s, { type: "acceptDraft" });
		// The list the run finishes with already carries it once; a second entry
		// would make apprise deliver the same alert twice to the same phone.
		expect(s.added).toEqual([]);
		expect(s.step).toBe(5);
	});

	it("drops the destinations added in this run when the apprise address changes", () => {
		let s = reducer(
			initialState(
				props({
					initialApiUrl: "http://apprise:8000",
					savedTargets: ["tgram://1/2"],
				}),
			),
			{ type: "setKind", kind: "ntfy", ntfyServer: "https://ntfy.example.com" },
		);
		s = reducer(s, { type: "setField", key: "topic", value: "one" });
		s = reducer(s, { type: "tested" });
		s = reducer(s, { type: "acceptDraft" });
		expect(s.added).toEqual(["ntfys://ntfy.example.com/one"]);
		expect(s.saved).toEqual(["tgram://1/2"]);

		// The destination was proven through the old apprise only, so pointing at a
		// different one drops it rather than carrying an unproven URL to Finish. The
		// saved targets are untouched: they are not this run's to remove.
		s = reducer(s, { type: "setApiUrl", value: "http://apprise-b:8000" });
		expect(s.added).toEqual([]);
		expect(s.draft.acceptedUrl).toBeNull();
		expect(s.saved).toEqual(["tgram://1/2"]);
	});

	it("counts a stored or already added destination as a duplicate, but not the draft's own row", () => {
		const base = initialState(
			props({
				initialApiUrl: "http://apprise:8000",
				savedTargets: ["tgram://1/2"],
			}),
		);
		const withUrl = (url: string) =>
			reducer(
				reducer(base, { type: "setKind", kind: "other", ntfyServer: "" }),
				{ type: "setField", key: "url", value: url },
			);

		// Nothing typed yet is not a duplicate of anything.
		expect(isDuplicate(base)).toBe(false);
		expect(isDuplicate(withUrl("tgram://9/9"))).toBe(false);
		// One of the stored destinations, and one this run already accepted.
		expect(isDuplicate(withUrl("tgram://1/2"))).toBe(true);
		expect(
			isDuplicate({ ...withUrl("tgram://9/9"), added: ["tgram://9/9"] }),
		).toBe(true);
		// A draft being edited back into the row it was accepted as is itself, so
		// re-accepting an edited destination still works.
		expect(
			isDuplicate({
				...withUrl("tgram://9/9"),
				added: ["tgram://9/9"],
				draft: { ...withUrl("tgram://9/9").draft, acceptedUrl: "tgram://9/9" },
			}),
		).toBe(false);
	});

	// Model Hotel writes no alert_events row until something is chosen, so an
	// absent key is "nothing has been decided yet" and a stored blank is the
	// operator having turned every event off.
	it("seeds the recommended events only when the key has never been written", () => {
		expect([...initialState(props({ savedEvents: null })).events]).toEqual([
			"circuit_breaker_open",
			"circuit_breaker_closed",
		]);
		expect([...initialState(props({ savedEvents: "" })).events]).toEqual([]);
		expect([
			...initialState(props({ savedEvents: "fleet_conflict" })).events,
		]).toEqual(["fleet_conflict"]);
		// An "Add destination" run never seeds: it is not the run that decides.
		expect([
			...initialState(
				props({
					savedEvents: null,
					startAt: 2,
					initialApiUrl: "http://apprise:8000",
				}),
			).events,
		]).toEqual([]);
	});
});
