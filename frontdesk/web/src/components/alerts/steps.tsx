import type { TFunction } from "i18next";
import {
	type Dispatch,
	type ReactNode,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import type { AlertEventDef, AlertStatus } from "../../api/types";
import { generateTopic } from "../../utils/ntfy";
import { type Action, isDuplicate, type WizardState } from "./AlertsWizard";
import {
	APPRISE_SERVICES_URL,
	type DestinationKind,
	FIELDS,
	type FieldDef,
	parseDiscordWebhook,
	parseUnifiedPushEndpoint,
} from "./composers";
import { DestinationList } from "./DestinationList";
import { eventLabel, SEVERITY_COLOR } from "./events";

// The seven step bodies of the alerts wizard: prove apprise-api answers, pick
// what kind of destination this is, fill in the parts that kind needs, deliver
// one real test to it, review the destination list, choose the events, and
// write. They are presentation only; every gate lives in AlertsWizard's
// canNext, so a step can never let itself through, and only the last one asks
// the wizard to persist anything.

const K = "settings.alerts.wizard";

export interface StepProps {
	state: WizardState;
	dispatch: Dispatch<Action>;
	t: TFunction;
}

// StepTitle names the step, and is the only part of the wizard that announces
// itself. The live region is deliberately this small: over the whole step body
// it would read every keystroke in the destination fields back at the operator,
// while over the title alone it says which of the seven steps the run just
// moved to. The wrapper carries the role so the heading stays a heading, and
// the role="alert" nodes inside the body (a failed test, a rejected Finish) are
// outside this region and keep announcing themselves.
function StepTitle({ id, children }: { id?: string; children: ReactNode }) {
	return (
		<div role="status">
			<h3 className="fd-step-title" id={id}>
				{children}
			</h3>
		</div>
	);
}

export function StepApprise({
	state,
	dispatch,
	t,
	onCheck,
}: StepProps & { onCheck: () => void }) {
	// A result only describes the URL it was taken against, so it disappears the
	// moment the field is edited rather than lingering as a stale green tick.
	const status = state.apiUrl === state.probedUrl ? state.apiStatus : null;
	return (
		<>
			<StepTitle>{t(`${K}.step1Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step1Hint`)}</p>
			<div className="ui-field">
				<label className="ui-label" htmlFor="wiz-api-url">
					{t(`${K}.apiUrlLabel`)}
				</label>
				<div className="fd-row" style={{ gap: "0.4rem" }}>
					<input
						id="wiz-api-url"
						data-testid="wiz-api-url"
						className="ui-input"
						type="text"
						value={state.apiUrl}
						onChange={(e) =>
							dispatch({ type: "setApiUrl", value: e.target.value })
						}
					/>
					<button
						type="button"
						className="ui-btn"
						data-testid="wiz-api-check"
						disabled={state.apiChecking || state.apiUrl.trim() === ""}
						onClick={onCheck}
					>
						{state.apiChecking ? t(`${K}.checking`) : t(`${K}.check`)}
					</button>
				</div>
			</div>
			{state.added.length > 0 && (
				<p
					data-testid="wiz-api-changed-drops"
					style={{ color: "var(--warn)", fontSize: "0.82rem" }}
				>
					{t(`${K}.apiChangedDrops`)}
				</p>
			)}
			{status && (
				<p
					role="status"
					data-testid="wiz-api-status"
					data-ok={status.healthy ? "true" : "false"}
					className={status.healthy ? "fd-ok-text" : "fd-error-text"}
				>
					{status.healthy
						? t(`${K}.apiOk`)
						: reasonText(status.reason ?? "", t, t(`${K}.apiFailed`))}
				</p>
			)}
		</>
	);
}

const KINDS: DestinationKind[] = [
	"ntfy",
	"bellhop",
	"telegram",
	"discord",
	"email",
	"other",
];

const KIND_HINT: Record<DestinationKind, string> = {
	ntfy: "kindNtfyHint",
	bellhop: "kindBellhopHint",
	telegram: "kindTelegramHint",
	discord: "kindDiscordHint",
	email: "kindEmailHint",
	other: "kindOtherHint",
};

// Four tiles are named after the service itself, so they reuse the shared
// settings.alerts.kind.* labels. Two need more than the bare name to be
// picked correctly: "ntfy" means nothing until it says it is the phone app,
// and "Apprise URL" is the catch-all rather than a service.
const KIND_TITLE: Partial<Record<DestinationKind, string>> = {
	ntfy: "kindNtfyTitle",
	other: "kindOtherTitle",
};

export function StepKind({
	state,
	dispatch,
	t,
	ntfyServer,
}: StepProps & { ntfyServer: string }) {
	return (
		<>
			<StepTitle id="wiz-kind-title">{t(`${K}.step2Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step2Hint`)}</p>
			{/* "Add another" is one click, so undoing it has to be one click too:
			    Back walks the run's own order (towards the apprise address), which
			    is not where a second destination was started from. */}
			{state.added.length > 0 && (
				<div>
					<button
						type="button"
						className="fd-link"
						data-testid="wiz-back-to-list"
						onClick={() => dispatch({ type: "go", step: 5 })}
					>
						{t(`${K}.backToList`)}
					</button>
				</div>
			)}
			<div
				role="radiogroup"
				aria-labelledby="wiz-kind-title"
				className="fd-stack"
				style={{ gap: "0.4rem" }}
			>
				{KINDS.map((kind) => {
					const selected = state.draft.kind === kind;
					return (
						<label
							key={kind}
							className="ui-card ui-card-pad fd-row"
							style={{
								gap: "0.6rem",
								alignItems: "flex-start",
								cursor: "pointer",
								borderColor: selected ? "var(--accent)" : undefined,
							}}
						>
							<input
								type="radio"
								name="wiz-kind"
								data-testid={`wiz-kind-${kind}`}
								checked={selected}
								onChange={() => dispatch({ type: "setKind", kind, ntfyServer })}
							/>
							<span>
								<span style={{ display: "block", fontWeight: 600 }}>
									{KIND_TITLE[kind]
										? t(`${K}.${KIND_TITLE[kind]}`)
										: t(`settings.alerts.kind.${kind}`)}
								</span>
								<span
									className="fd-faint"
									style={{ display: "block", fontSize: "0.82rem" }}
								>
									{t(`${K}.${KIND_HINT[kind]}`)}
								</span>
							</span>
						</label>
					);
				})}
			</div>
		</>
	);
}

export function StepDetails({ state, dispatch, t }: StepProps) {
	const kind = state.draft.kind;
	if (kind === null) return null;

	const value = (key: string) => state.draft.fields[key] ?? "";
	const set = (key: string, v: string) =>
		dispatch({ type: "setField", key, value: v });

	const parsed =
		kind === "bellhop" ? parseUnifiedPushEndpoint(value("endpoint")) : null;
	const discord =
		kind === "discord" ? parseDiscordWebhook(value("webhook")) : null;

	// This exact URL is already on the list the run will finish with, either
	// stored or accepted earlier in this run. The step 3 gate refuses it, so
	// this line explains a Next button that will not move until the destination
	// is changed into a new one.
	const duplicate = isDuplicate(state);

	return (
		<>
			<StepTitle>{t(`${K}.step3Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step3Hint`)}</p>

			{FIELDS[kind].map((f) => (
				<Field
					key={f.key}
					def={f}
					label={t(`${K}.field.${f.key}`)}
					value={value(f.key)}
					onChange={(v) => set(f.key, v)}
				>
					{kind === "ntfy" && f.key === "topic" && (
						<button
							type="button"
							className="ui-btn"
							data-testid="wiz-generate-topic"
							onClick={() => set("topic", generateTopic())}
						>
							{t(`${K}.generate`)}
						</button>
					)}
				</Field>
			))}

			{/* Nothing here checks that the ntfy server answers: Front Desk serves
			    `connect-src 'self'`, so a fetch at the operator's own server is
			    blocked before it leaves the page and could only ever report
			    failure. Step 4 sends from Front Desk, which is the side that has
			    to reach the server anyway. */}
			{kind === "ntfy" && (
				<>
					<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
						{t(`${K}.ntfyServerHint`)}
					</p>
					<div className="fd-stack" style={{ gap: "0.3rem" }}>
						<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
							{t(`${K}.ntfySubscribe`)}
						</p>
						<CopyRow
							testId="wiz-copy-server"
							label={t(`${K}.field.server`)}
							value={value("server")}
							t={t}
						/>
						<CopyRow
							testId="wiz-copy-topic"
							label={t(`${K}.field.topic`)}
							value={value("topic")}
							t={t}
						/>
					</div>
				</>
			)}

			{kind === "bellhop" && (
				<>
					<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
						{t(`${K}.bellhopPasteHint`)}
					</p>
					{parsed && (
						<div data-testid="wiz-bellhop-parsed" className="fd-stack">
							<code className="fd-mono" style={{ fontSize: "0.8rem" }}>
								{parsed.server}
							</code>
							<code className="fd-mono" style={{ fontSize: "0.8rem" }}>
								{parsed.topic}
							</code>
						</div>
					)}
					{!parsed && value("endpoint").trim() !== "" && (
						<p
							data-testid="wiz-bellhop-error"
							role="status"
							className="fd-error-text"
						>
							{t(`${K}.bellhopBad`)}
						</p>
					)}
					<p
						data-testid="wiz-bellhop-note"
						className="fd-faint"
						style={{ fontSize: "0.82rem" }}
					>
						{t(`${K}.bellhopNote`)}
					</p>
				</>
			)}

			{/* Next is gated on a composed URL, so an unparseable webhook otherwise
			    leaves the operator with a dead button and no reason for it. */}
			{kind === "discord" && !discord && value("webhook").trim() !== "" && (
				<p
					data-testid="wiz-discord-error"
					role="status"
					className="fd-error-text"
				>
					{t(`${K}.discordBad`)}
				</p>
			)}

			{kind === "other" && (
				<a
					className="fd-link"
					href={APPRISE_SERVICES_URL}
					target="_blank"
					rel="noreferrer"
					style={{ fontSize: "0.82rem" }}
				>
					{t(`${K}.otherBrowse`)}
				</a>
			)}

			<Composed url={state.draft.url} t={t} />

			{duplicate && (
				<p
					data-testid="wiz-already-saved"
					role="alert"
					className="fd-error-text"
					style={{ fontSize: "0.82rem" }}
				>
					{t(`${K}.alreadySaved`)}
				</p>
			)}
		</>
	);
}

export function StepTest({
	state,
	t,
	onSendTest,
}: StepProps & { onSendTest: () => void }) {
	const kind = state.draft.kind ?? "other";
	return (
		<>
			<StepTitle>{t(`${K}.step4Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step4Hint`)}</p>
			<Composed url={state.draft.url} t={t} />
			<div>
				<button
					type="button"
					className="ui-btn ui-btn-primary"
					data-testid="wiz-send-test"
					disabled={state.testing || state.draft.url === ""}
					onClick={onSendTest}
				>
					{state.testing ? t(`${K}.sending`) : t(`${K}.sendTest`)}
				</button>
			</div>
			{(state.testOk || state.testError !== "") && (
				<p
					role="status"
					data-testid="wiz-test-result"
					data-ok={state.testOk ? "true" : "false"}
					className={state.testOk ? "fd-ok-text" : "fd-error-text"}
				>
					{state.testOk
						? t(`${K}.testSent.${kind}`)
						: reasonText(state.testError, t, t("settings.alerts.testFailed"))}
				</p>
			)}
			{!state.draft.tested && (
				<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
					{t(`${K}.testMustPass`)}
				</p>
			)}
		</>
	);
}

export function StepDestinations({
	state,
	dispatch,
	t,
	savedTargets,
	onTestRow,
}: StepProps & {
	/** Already-stored destinations, which this step counts but never lists. */
	savedTargets: string[];
	/** Deliver one test to this destination through the wizard's apprise URL. */
	onTestRow: (url: string) => Promise<void>;
}) {
	// Which row was last tested from here and how it went. It is a per-row
	// courtesy check on a list nothing is gated on, so it stays local rather
	// than joining the wizard's state machine.
	const [rowTest, setRowTest] = useState<{
		url: string;
		state: "sending" | "ok" | "failed";
	} | null>(null);

	const run = (url: string) => {
		setRowTest({ url, state: "sending" });
		onTestRow(url).then(
			() => setRowTest({ url, state: "ok" }),
			() => setRowTest({ url, state: "failed" }),
		);
	};

	return (
		<>
			<StepTitle>{t(`${K}.step5Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step5Hint`)}</p>
			{/* The stored destinations are not this run's work and not this run's to
			    delete, so they are counted rather than listed: the list below is
			    what this run adds, and every row on it can be taken back off. */}
			{savedTargets.length > 0 && (
				<p
					data-testid="wiz-saved-note"
					className="fd-faint"
					style={{ fontSize: "0.82rem" }}
				>
					{t(`${K}.savedNote`, { count: savedTargets.length })}
				</p>
			)}
			<DestinationList
				targets={state.added}
				onRemove={(url) => dispatch({ type: "dropAdded", url })}
				onTest={run}
				busy={rowTest?.state === "sending"}
				emptyText={t(`${K}.nothingAdded`)}
			/>
			{rowTest && rowTest.state !== "sending" && (
				<p
					role="status"
					data-testid="wiz-row-test-result"
					data-ok={rowTest.state === "ok" ? "true" : "false"}
					className={rowTest.state === "ok" ? "fd-ok-text" : "fd-error-text"}
					style={{ fontSize: "0.82rem" }}
				>
					{rowTest.state === "ok"
						? t("settings.alerts.testSent")
						: t("settings.alerts.testFailed")}
				</p>
			)}
			<div>
				<button
					type="button"
					className="ui-btn"
					data-testid="wiz-add-another"
					onClick={() => dispatch({ type: "newDraft" })}
				>
					{t(`${K}.addAnother`)}
				</button>
			</div>
		</>
	);
}

export function StepEvents({
	state,
	dispatch,
	t,
	catalog,
}: StepProps & { catalog: AlertEventDef[] }) {
	// Grouped by the catalog's own (English) category, exactly as the card's
	// picker does, so the two lists read the same way.
	const grouped = useMemo(() => {
		const m = new Map<string, AlertEventDef[]>();
		for (const e of catalog) {
			const g = m.get(e.category) ?? [];
			g.push(e);
			m.set(e.category, g);
		}
		return [...m.entries()];
	}, [catalog]);

	return (
		<>
			<StepTitle>{t(`${K}.step6Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step6Hint`)}</p>
			{grouped.map(([category, defs]) => (
				<div key={category} style={{ marginBottom: "0.6rem" }}>
					<div style={{ fontWeight: 500, fontSize: "0.85rem" }}>{category}</div>
					{defs.map((d) => {
						const label = eventLabel(t, d.type);
						return (
							<label
								key={d.type}
								className="fd-row"
								style={{ cursor: "pointer", marginTop: "0.2rem" }}
							>
								<input
									type="checkbox"
									data-testid={`wiz-event-${d.type}`}
									aria-label={label}
									checked={state.events.has(d.type)}
									onChange={(e) =>
										dispatch({
											type: "toggleEvent",
											eventType: d.type,
											on: e.target.checked,
										})
									}
								/>
								<span
									aria-hidden="true"
									style={{
										display: "inline-block",
										width: "0.5rem",
										height: "0.5rem",
										borderRadius: "50%",
										background:
											SEVERITY_COLOR[d.severity] ?? "var(--text-faint)",
									}}
								/>
								<span style={{ fontSize: "0.85rem" }}>{label}</span>
							</label>
						);
					})}
				</div>
			))}
			{/* An empty selection is a legitimate choice ("set up now, decide what
			    to hear about later"), so it is a note rather than a gate. */}
			{state.events.size === 0 && (
				<p
					data-testid="wiz-none-selected"
					style={{ color: "var(--warn)", fontSize: "0.82rem" }}
				>
					{t(`${K}.noneSelected`)}
				</p>
			)}
			<div>
				<button
					type="button"
					className="fd-link"
					data-testid="wiz-reset-recommended"
					onClick={() =>
						dispatch({
							type: "resetEvents",
							types: catalog.filter((e) => e.defaultOn).map((e) => e.type),
						})
					}
				>
					{t(`${K}.resetRecommended`)}
				</button>
			</div>
		</>
	);
}

export function StepFinish({
	state,
	t,
	targets,
	onSendAll,
}: StepProps & { targets: string[]; onSendAll: () => void }) {
	if (state.done) {
		return (
			<>
				<p role="status" data-testid="wiz-done" className="fd-row">
					<span className="ui-badge ui-badge-ok">{t(`${K}.done`)}</span>
					{/* Read back from the server after the write, so the pill describes
					    the stored configuration rather than the one just typed. */}
					{state.finalStatus && <FinalPill status={state.finalStatus} t={t} />}
				</p>
				<div>
					<button
						type="button"
						className="ui-btn"
						data-testid="wiz-send-all"
						disabled={state.sendingAll}
						onClick={onSendAll}
					>
						{state.sendingAll ? t(`${K}.sending`) : t(`${K}.sendAll`)}
					</button>
				</div>
				{state.sentAll !== "none" && (
					<p
						role="status"
						data-testid="wiz-sent-all"
						data-ok={state.sentAll === "ok" ? "true" : "false"}
						className={state.sentAll === "ok" ? "fd-ok-text" : "fd-error-text"}
						style={{ fontSize: "0.82rem" }}
					>
						{state.sentAll === "ok"
							? t(`${K}.sentAll`)
							: t("settings.alerts.testFailed")}
					</p>
				)}
			</>
		);
	}
	return (
		<>
			<StepTitle>{t(`${K}.step7Title`)}</StepTitle>
			<p className="fd-faint fd-step-intro">{t(`${K}.step7Hint`)}</p>
			{/* Trimmed, because the summary promises what the write will store. */}
			<Summary
				label={t("settings.alerts.apiUrlLabel")}
				value={state.apiUrl.trim()}
			/>
			<Summary
				label={t("settings.alerts.destinationsTitle")}
				value={targets.join("; ")}
				testId="wiz-summary-targets"
			/>
			<Summary
				label={t("settings.alerts.eventsLabel")}
				value={
					state.events.size === 0
						? t(`${K}.noneSelected`)
						: [...state.events].map((type) => eventLabel(t, type)).join(", ")
				}
			/>
			{state.finishError !== "" && (
				<p
					role="alert"
					data-testid="wiz-finish-error"
					className="fd-error-text"
				>
					{state.finishError}
				</p>
			)}
		</>
	);
}

// FinalPill reports the probe taken straight after the write. The card's own
// pill covers a fourth state (nothing configured at all) that cannot happen
// here: the wizard has just configured it.
function FinalPill({ status, t }: { status: AlertStatus; t: TFunction }) {
	const [variant, label] = !status.reachable
		? (["ui-badge-danger", "statusUnreachable"] as const)
		: !status.healthy
			? (["ui-badge-warn", "statusUnhealthy"] as const)
			: (["ui-badge-ok", "statusOk"] as const);
	return (
		<span
			className={`ui-badge ${variant}`}
			data-testid="wiz-done-pill"
			title={status.detail}
		>
			{t(`settings.alerts.${label}`)}
		</span>
	);
}

// One labelled line of the closing summary: what is about to be written, in the
// operator's own words rather than as the settings keys it becomes.
function Summary({
	label,
	value,
	testId,
}: {
	label: string;
	value: string;
	testId?: string;
}) {
	return (
		<div className="ui-field">
			<span className="ui-label">{label}</span>
			<span
				data-testid={testId}
				style={{ fontSize: "0.85rem", wordBreak: "break-all" }}
			>
				{value}
			</span>
		</div>
	);
}

// reasonText renders a server reason code. Codes the catalog does not cover
// (and a failure that carried none) fall back to the caller's own wording
// rather than leaking a raw key, or a sentence about the wrong thing, into the
// dialog: a probe that could not be made is not a test that failed to deliver.
function reasonText(code: string, t: TFunction, fallback: string): string {
	return t(`settings.alerts.reason.${code}`, { defaultValue: fallback });
}

// The composed Apprise URL, shown from the moment the fields make a valid one.
// It is the thing that gets tested and stored, so the operator sees it rather
// than having to trust that the fields were assembled the way they expect.
function Composed({ url, t }: { url: string; t: TFunction }) {
	if (url === "") return null;
	return (
		<div className="ui-field">
			<span className="ui-label">{t(`${K}.composedLabel`)}</span>
			<code
				className="fd-mono"
				data-testid="wiz-composed"
				style={{
					fontSize: "0.8rem",
					userSelect: "all",
					wordBreak: "break-all",
				}}
			>
				{url}
			</code>
		</div>
	);
}

function Field({
	def,
	label,
	value,
	onChange,
	children,
}: {
	def: FieldDef;
	label: string;
	value: string;
	onChange: (v: string) => void;
	children?: ReactNode;
}) {
	const id = `wiz-field-${def.key}`;
	return (
		<div className="ui-field">
			<label className="ui-label" htmlFor={id}>
				{label}
			</label>
			<div className="fd-row" style={{ gap: "0.4rem" }}>
				<input
					id={id}
					data-testid={id}
					className="ui-input"
					// Secrets are hidden while they are typed, which is the only moment
					// someone could be looking over a shoulder; nothing is masked after.
					type={def.secret ? "password" : "text"}
					placeholder={def.placeholder}
					value={value}
					onChange={(e) => onChange(e.target.value)}
				/>
				{children}
			</div>
		</div>
	);
}

// One value the operator has to retype on a phone, with a button that saves the
// retyping. A blocked clipboard is silent: the text stays selectable.
function CopyRow({
	testId,
	label,
	value,
	t,
}: {
	testId: string;
	label: string;
	value: string;
	t: TFunction;
}) {
	const [copied, setCopied] = useState(false);
	// The "Copied" label reverts on a timer, which has to be dropped if the step
	// is left first: the wizard swaps steps under it, and firing then would set
	// state on an unmounted row.
	const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(
		() => () => {
			if (timer.current !== null) clearTimeout(timer.current);
		},
		[],
	);
	if (value === "") return null;
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(true);
			if (timer.current !== null) clearTimeout(timer.current);
			timer.current = setTimeout(() => setCopied(false), 2000);
		} catch {
			/* clipboard blocked: the value stays selectable */
		}
	};
	return (
		<div className="fd-row" style={{ gap: "0.4rem", alignItems: "center" }}>
			<span className="fd-faint" style={{ fontSize: "0.8rem" }}>
				{label}
			</span>
			<code
				className="fd-mono"
				style={{ fontSize: "0.8rem", userSelect: "all" }}
			>
				{value}
			</code>
			<button
				type="button"
				className="ui-btn ui-btn-sm"
				data-testid={testId}
				aria-label={`${t(`${K}.copy`)}: ${label}`}
				onClick={copy}
			>
				{copied ? t(`${K}.copied`) : t(`${K}.copy`)}
			</button>
		</div>
	);
}
