import { screen, waitFor, within } from "@testing-library/react";
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

// The same catalog the mocked GET /api/alert/events serves, because step 6
// renders the card's own picker: the wizard is handed the catalog for the
// recommended preset while the picker reads it from the API, and the two
// disagreeing would make the step describe events it cannot tick.
const catalog: AlertEventDef[] = [
	{
		type: "circuit_breaker.open",
		category: "Failover",
		severity: "warning",
		defaultOn: true,
	},
	{
		type: "circuit_breaker.closed",
		category: "Failover",
		severity: "success",
		defaultOn: true,
	},
	{
		type: "discovery.provider_failed",
		category: "Discovery",
		severity: "error",
		defaultOn: false,
	},
];

/** The picker's checkbox for one event type, addressed by role inside its row. */
function eventBox(type: string): HTMLInputElement {
	return within(screen.getByTestId(`alert-event-${type}`)).getByRole(
		"checkbox",
	);
}

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

/** What GET /api/alert/targets reports as stored, which Finish re-reads. */
function storedTargets(targets: string[]) {
	server.use(
		http.get("/api/alert/targets", () => HttpResponse.json({ targets })),
	);
}

/** Every settings write the run makes, in order. */
function capturePuts() {
	const puts: Record<string, unknown>[] = [];
	server.use(
		http.put("/api/settings", async ({ request }) => {
			puts.push((await request.json()) as Record<string, unknown>);
			return HttpResponse.json({});
		}),
	);
	return puts;
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

	// Model Hotel writes no alert_events row until something is chosen and runs on
	// the recommended defaults until then, so an absent key seeds that same set
	// whichever step the run starts on. A stored blank is the operator having
	// turned every event off, and stays off.
	it("seeds the recommended events only when the key has never been written", () => {
		expect([...initialState(props({ savedEvents: null })).events]).toEqual([
			"circuit_breaker.open",
			"circuit_breaker.closed",
		]);
		expect([...initialState(props({ savedEvents: "" })).events]).toEqual([]);
		expect([
			...initialState(props({ savedEvents: "discovery.provider_failed" }))
				.events,
		]).toEqual(["discovery.provider_failed"]);
		// Which step the run starts on does not change what is stored, so an
		// "Add destination" run reads the same absent key the same way.
		expect([
			...initialState(
				props({
					savedEvents: null,
					startAt: 2,
					initialApiUrl: "http://apprise:8000",
				}),
			).events,
		]).toEqual(["circuit_breaker.open", "circuit_breaker.closed"]);
		expect([
			...initialState(
				props({
					savedEvents: "",
					startAt: 2,
					initialApiUrl: "http://apprise:8000",
				}),
			).events,
		]).toEqual([]);
	});

	it("steps 5-7: lists this run's additions, carries the saved events, and writes once at Finish", async () => {
		const testBodies: string[] = [];
		healthyProbe();
		storedTargets(["tgram://1/2"]);
		const puts = capturePuts();
		server.use(
			http.post("/api/alert/test", async ({ request }) => {
				testBodies.push(await request.text());
				return HttpResponse.json({ ok: true });
			}),
			http.get("/api/alert/status", () =>
				HttpResponse.json({ configured: true, reachable: true, healthy: true }),
			),
		);
		const { onFinished } = renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "circuit_breaker.open",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5

		// The list is this run's work only: the one destination just proven, which
		// can be dropped again. The stored one is counted in a note instead,
		// because the wizard never removes stored destinations.
		expect(screen.getAllByTestId("alert-destination-row")).toHaveLength(1);
		expect(screen.getByTestId("wiz-saved-note")).toHaveTextContent(
			i18n.t("settings.alerts.wizard.savedNote", { count: 1 }),
		);

		// Any row can be tried on its own from here, through the URL this run
		// proved rather than the stored configuration.
		await userEvent.click(screen.getByTestId("alert-destination-test"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-row-test-result")).toHaveAttribute(
				"data-ok",
				"true",
			),
		);
		expect(JSON.parse(testBodies.at(-1) ?? "{}")).toEqual({
			api_url: "http://apprise:8000",
			targets: ["ntfys://ntfy.example.com/abcabcabc"],
		});

		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		expect(eventBox("circuit_breaker.open")).toBeChecked();
		expect(eventBox("discovery.provider_failed")).not.toBeChecked();
		await userEvent.click(eventBox("discovery.provider_failed"));
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7

		expect(puts).toHaveLength(0);
		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-done")).toBeInTheDocument(),
		);
		// The closing pill reports the probe taken after the write, not before it.
		expect(screen.getByTestId("wiz-done-pill")).toHaveTextContent(
			i18n.t("settings.alerts.status.reachable"),
		);
		expect(puts).toEqual([
			{
				alert_apprise_api_url: "http://apprise:8000",
				alert_apprise_targets:
					"tgram://1/2; ntfys://ntfy.example.com/abcabcabc",
				alert_enabled: "true",
				alert_events: "circuit_breaker.open,discovery.provider_failed",
			},
		]);

		// "Send test to everything" exercises what is now stored, so it carries no
		// body at all.
		await userEvent.click(screen.getByTestId("wiz-send-all"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-sent-all")).toHaveAttribute(
				"data-ok",
				"true",
			),
		);
		expect(testBodies.at(-1)).toBe("");

		await userEvent.click(screen.getByTestId("wiz-close"));
		expect(onFinished).toHaveBeenCalled();
	});

	// Config sync owns alerting on/off and the event routing fleet-wide, so a
	// managed member's run writes only what is local to it.
	it("skips the event step and writes only the destination settings when managed", async () => {
		healthyProbe();
		passingTest();
		storedTargets([]);
		const puts = capturePuts();
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedEvents: "circuit_breaker.open",
			managed: true,
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6

		expect(screen.getByTestId("wiz-managed-events")).toHaveTextContent(
			i18n.t("settings.alerts.wizard.managedEventsNote"),
		);
		expect(screen.queryByTestId("alert-event-picker")).toBeNull();
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
		// Nothing on this member decides the events, so the summary does not
		// promise a selection the write will not carry.
		expect(screen.queryByTestId("wiz-summary-events")).toBeNull();

		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-done")).toBeInTheDocument(),
		);
		expect(puts).toEqual([
			{
				alert_apprise_api_url: "http://apprise:8000",
				alert_apprise_targets: "ntfys://ntfy.example.com/abcabcabc",
			},
		]);
	});

	// The write landed, so the run is over either way; the pill is where an
	// apprise that answers but reports a problem is reported honestly.
	it("reports a saved but unhealthy apprise on the closing pill", async () => {
		healthyProbe();
		passingTest();
		storedTargets([]);
		capturePuts();
		server.use(
			http.get("/api/alert/status", () =>
				HttpResponse.json({
					configured: true,
					reachable: true,
					healthy: false,
					reason: "unhealthy",
					detail: "apprise reports a problem",
				}),
			),
		);
		renderWizard({ initialApiUrl: "http://apprise:8000", startAt: 2 });
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
		await userEvent.click(screen.getByTestId("wiz-finish"));

		const pill = await screen.findByTestId("wiz-done-pill");
		expect(pill).toHaveTextContent(i18n.t("settings.alerts.status.issues"));
		// Themed by what the pill says, not by a palette utility.
		expect(pill).toHaveClass("ui-badge", "ui-badge-warning");
		// The raw server text stays a tooltip so it never becomes the message.
		expect(pill).toHaveAttribute("title", "apprise reports a problem");
	});

	it("recovers from a rejected Finish, a failed probe read and a failed final test", async () => {
		healthyProbe();
		passingTest();
		storedTargets(["tgram://1/2"]);
		server.use(
			http.put("/api/settings", () =>
				HttpResponse.json(
					{ error: "apprise url must be http(s)" },
					{ status: 400 },
				),
			),
		);
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "circuit_breaker.open",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5

		// A row test that does not deliver says so without touching any gate.
		server.use(
			http.post("/api/alert/test", () =>
				HttpResponse.json(
					{ code: "deliver_failed", error: "x" },
					{ status: 502 },
				),
			),
		);
		await userEvent.click(screen.getByTestId("alert-destination-test"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-row-test-result")).toHaveAttribute(
				"data-ok",
				"false",
			),
		);
		expect(screen.getByTestId("wiz-next")).toBeEnabled();

		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-finish-error")).toHaveTextContent(
				"apprise url must be http(s)",
			),
		);
		// Nothing was written, so the run stays put and Finish can be pressed again.
		expect(screen.getByTestId("wiz-step-7")).toBeInTheDocument();
		expect(screen.getByTestId("wiz-finish")).toBeEnabled();

		// Second attempt: the write lands, but the probe read after it does not.
		// The configuration is saved either way, so the run finishes without a
		// pill rather than reporting a failure that did not happen.
		server.use(
			http.put("/api/settings", () => HttpResponse.json({})),
			http.get(
				"/api/alert/status",
				() => new HttpResponse(null, { status: 500 }),
			),
		);
		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-done")).toBeInTheDocument(),
		);
		expect(screen.queryByTestId("wiz-done-pill")).toBeNull();
		expect(screen.queryByTestId("wiz-finish-error")).toBeNull();

		// The closing test still reports honestly; the configuration is saved.
		await userEvent.click(screen.getByTestId("wiz-send-all"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-sent-all")).toHaveAttribute(
				"data-ok",
				"false",
			),
		);
		expect(screen.getByTestId("wiz-close")).toBeEnabled();
	});

	it("Add another returns to the list, and a row added here can be dropped again", async () => {
		healthyProbe();
		passingTest();
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			startAt: 2,
		});
		// The first destination has nowhere to go back to, so the escape hatch is
		// not offered before there is a list to return to.
		expect(screen.queryByTestId("wiz-back-to-list")).toBeNull();
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		expect(screen.getAllByTestId("alert-destination-row")).toHaveLength(1);

		// Starting a second destination and changing your mind comes straight back
		// to the list rather than stranding the run on an empty step 2.
		await userEvent.click(screen.getByTestId("wiz-add-another"));
		expect(screen.getByTestId("wiz-step-2")).toBeInTheDocument();
		expect(screen.getByTestId("wiz-next")).toBeDisabled();
		await userEvent.click(screen.getByTestId("wiz-back-to-list"));
		expect(screen.getByTestId("wiz-step-5")).toBeInTheDocument();

		// Back off the list now skips the abandoned draft's test step, which has no
		// destination left to talk about, and lands where one is started instead.
		await userEvent.click(screen.getByTestId("wiz-back"));
		expect(screen.getByTestId("wiz-step-2")).toBeInTheDocument();
		await userEvent.click(screen.getByTestId("wiz-back-to-list"));

		// Dropping the row this run added empties the list, and leaves the stored
		// destination exactly where it was: it is only ever counted here.
		await userEvent.click(screen.getByTestId("alert-destination-remove"));
		await userEvent.click(
			screen.getByTestId("alert-destination-remove-confirm"),
		);
		// The confirmation animates itself out and drops the row on the way.
		await waitFor(() =>
			expect(screen.queryAllByTestId("alert-destination-row")).toHaveLength(0),
		);
		expect(screen.getByTestId("alert-destinations-empty")).toHaveTextContent(
			i18n.t("settings.alerts.wizard.nothingAdded"),
		);
		expect(screen.getByTestId("wiz-saved-note")).toBeInTheDocument();
		// One stored destination is still a destination, so the run can continue.
		expect(screen.getByTestId("wiz-next")).toBeEnabled();
	});

	it("step 6 resets to the recommended preset and warns when nothing is ticked", async () => {
		healthyProbe();
		passingTest();
		// A stored selection wins over the recommended set on entry.
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "discovery.provider_failed",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		expect(eventBox("discovery.provider_failed")).toBeChecked();
		expect(eventBox("circuit_breaker.open")).not.toBeChecked();
		expect(screen.queryByTestId("wiz-none-selected")).toBeNull();

		await userEvent.click(eventBox("discovery.provider_failed"));
		expect(screen.getByTestId("wiz-none-selected")).toHaveClass(
			"ui-callout",
			"ui-callout-warning",
		);
		// An empty selection is allowed through: it means "configured, notify me
		// about nothing yet", which the note says out loud on both steps.
		expect(screen.getByTestId("wiz-next")).toBeEnabled();
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
		expect(screen.getByTestId("wiz-summary-events")).toHaveTextContent(
			i18n.t("settings.alerts.wizard.noneSelected"),
		);
		await userEvent.click(screen.getByTestId("wiz-back")); // -> 6

		await userEvent.click(screen.getByTestId("wiz-reset-recommended"));
		expect(eventBox("circuit_breaker.open")).toBeChecked();
		expect(eventBox("circuit_breaker.closed")).toBeChecked();
		expect(eventBox("discovery.provider_failed")).not.toBeChecked();
		expect(screen.queryByTestId("wiz-none-selected")).toBeNull();
	});

	it("keeps a deliberately empty event selection empty when adding a destination", async () => {
		healthyProbe();
		passingTest();
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");

		expect(eventBox("circuit_breaker.open")).not.toBeChecked();
		expect(eventBox("circuit_breaker.closed")).not.toBeChecked();
		// Nothing is ticked, so the step says so rather than looking half-loaded.
		expect(screen.getByTestId("wiz-none-selected")).toBeInTheDocument();
		// The recommended set is one click away whenever it is wanted.
		await userEvent.click(screen.getByTestId("wiz-reset-recommended"));
		expect(eventBox("circuit_breaker.open")).toBeChecked();
	});

	it("seeds the recommended preset on a setup run with nothing written yet", async () => {
		healthyProbe();
		passingTest();
		renderWizard({ savedEvents: null });
		await userEvent.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 2
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");

		expect(eventBox("circuit_breaker.open")).toBeChecked();
		expect(eventBox("circuit_breaker.closed")).toBeChecked();
		expect(eventBox("discovery.provider_failed")).not.toBeChecked();
		expect(screen.queryByTestId("wiz-none-selected")).toBeNull();
	});

	// The write replaces the whole destination list, and the wizard's copy of it
	// is as old as the dialog: a destination saved elsewhere while the run was
	// open would be written away. Finish re-reads the stored list first.
	it("keeps destinations saved elsewhere while the wizard was open", async () => {
		let releaseWrite = () => {};
		const writeGate = new Promise<void>((resolve) => {
			releaseWrite = resolve;
		});
		const puts: Record<string, unknown>[] = [];
		healthyProbe();
		passingTest();
		server.use(
			http.get("/api/alert/status", () =>
				HttpResponse.json({ configured: true, reachable: true, healthy: true }),
			),
			// What is stored by the time Finish is pressed: the destination the
			// wizard opened with, plus one another tab added in the meantime.
			http.get("/api/alert/targets", () =>
				HttpResponse.json({
					targets: ["tgram://1/2", "ntfys://ntfy.example.com/added-elsewhere"],
				}),
			),
			// Held open so the summary can be read while the write is in flight.
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				await writeGate;
				return HttpResponse.json({});
			}),
		);
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "circuit_breaker.open",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
		// Before Finish the summary can only promise what the wizard was handed.
		expect(screen.getByTestId("wiz-summary-targets")).toHaveTextContent(
			"tgram://1/2; ntfys://ntfy.example.com/abcabcabc",
		);

		await userEvent.click(screen.getByTestId("wiz-finish"));
		// The fresh list lands in state before the write, so the summary the
		// operator is looking at while it runs is what the write carries.
		await waitFor(() =>
			expect(screen.getByTestId("wiz-summary-targets")).toHaveTextContent(
				"tgram://1/2; ntfys://ntfy.example.com/added-elsewhere; ntfys://ntfy.example.com/abcabcabc",
			),
		);
		releaseWrite();
		await waitFor(() =>
			expect(screen.getByTestId("wiz-done")).toBeInTheDocument(),
		);

		expect(puts).toHaveLength(1);
		expect(puts[0].alert_apprise_targets).toBe(
			"tgram://1/2; ntfys://ntfy.example.com/added-elsewhere; ntfys://ntfy.example.com/abcabcabc",
		);
	});

	it("writes nothing when the stored destinations cannot be read at Finish", async () => {
		let reads = 0;
		healthyProbe();
		passingTest();
		const puts = capturePuts();
		server.use(
			// A rotated master key first, then a failure with nothing to say.
			http.get("/api/alert/targets", () => {
				reads += 1;
				return reads === 1
					? HttpResponse.json(
							{ code: "undecryptable", error: "cannot decrypt" },
							{ status: 500 },
						)
					: new HttpResponse(null, { status: 500 });
			}),
		);
		renderWizard({
			initialApiUrl: "http://apprise:8000",
			savedTargets: ["tgram://1/2"],
			savedEvents: "circuit_breaker.open",
			startAt: 2,
		});
		await addNtfy("https://ntfy.example.com", "abcabcabc"); // -> 5
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		await userEvent.click(screen.getByTestId("wiz-next")); // -> 7

		// A rotated master key is the one cause worth naming, because it tells the
		// operator what to do about it.
		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-finish-error")).toHaveTextContent(
				i18n.t("settings.alerts.destinations.error"),
			),
		);
		// The only write available would have dropped the stored destinations, so
		// none was attempted and the run stays where it can be tried again.
		expect(puts).toEqual([]);
		expect(screen.getByTestId("wiz-step-7")).toBeInTheDocument();
		expect(screen.getByTestId("wiz-finish")).toBeEnabled();

		// Any other read failure is not something the operator can act on, so it
		// is reported generically rather than blamed on the master key.
		await userEvent.click(screen.getByTestId("wiz-finish"));
		await waitFor(() =>
			expect(screen.getByTestId("wiz-finish-error")).toHaveTextContent(
				i18n.t("settings.alerts.destinations.readFailed"),
			),
		);
		expect(puts).toEqual([]);
	});
});
