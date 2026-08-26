import {
	APPRISE_SERVICES_URL,
	FIELDS,
	parseDiscordWebhook,
} from "@web-shared/alerts/composers";
import { eventLabel, parseCsv } from "@web-shared/alerts/events";
import { generateTopic } from "@web-shared/ntfy";
import { Fragment, useState } from "react";
import {
	Bell,
	CheckCircle2,
	CheckSquare,
	ListChecks,
	Pencil,
	PlugZap,
	Send,
	Smartphone,
} from "@/lib/icons";
import type { AlertEventDef } from "../../../api/types";
import { AlertEventPicker } from "../AlertEventPicker";
import { DestinationList } from "./DestinationList";
import { KIND_HINT, KIND_ICON, KIND_TITLE, KINDS } from "./destinationKinds";
import {
	Composed,
	CopyRow,
	Field,
	FinalPill,
	Mono,
	ResultLine,
	StepTitle,
	Summary,
} from "./stepPrimitives";
import { K, reasonText, type StepProps } from "./stepShared";
import { isDuplicate } from "./wizardState";

// The seven step bodies of the alerts wizard: prove apprise-api answers, pick
// what kind of destination this is, fill in the parts that kind needs, deliver
// one real test to it, review the destination list, choose the events, and
// write. They are presentation only; every gate lives in AlertsWizard's
// canNext, so a step can never let itself through, and only the last one asks
// the wizard to persist anything.

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
			<StepTitle icon={PlugZap}>{t(`${K}.step1Title`)}</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step1Hint`)}</p>
			<div className="space-y-1.5">
				<label
					className="text-sm font-medium text-(--text-secondary)"
					htmlFor="wiz-api-url"
				>
					{t(`${K}.apiUrlLabel`)}
				</label>
				<div className="flex items-center gap-2">
					<input
						id="wiz-api-url"
						data-testid="wiz-api-url"
						className="ui-input text-sm w-full"
						type="text"
						spellCheck={false}
						autoComplete="off"
						value={state.apiUrl}
						onChange={(e) =>
							dispatch({ type: "setApiUrl", value: e.target.value })
						}
					/>
					<button
						type="button"
						className="ui-btn ui-btn-secondary shrink-0"
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
					className="ui-callout ui-callout-warning"
				>
					{t(`${K}.apiChangedDrops`)}
				</p>
			)}
			{status && (
				<ResultLine ok={status.healthy} testId="wiz-api-status">
					{status.healthy
						? t(`${K}.apiOk`)
						: reasonText(status.reason ?? "", t, t(`${K}.apiFailed`))}
				</ResultLine>
			)}
		</>
	);
}
export function StepKind({
	state,
	dispatch,
	t,
	ntfyServer,
}: StepProps & { ntfyServer: string }) {
	return (
		<>
			<StepTitle id="wiz-kind-title" icon={Send}>
				{t(`${K}.step2Title`)}
			</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step2Hint`)}</p>
			{/* "Add another" is one click, so undoing it has to be one click too:
			    Back walks the run's own order (towards the apprise address), which
			    is not where a second destination was started from. It is offered
			    from the moment the run has seen the list, which includes coming
			    back here after removing every row that was on it. */}
			{state.listSeen && (
				<div>
					<button
						type="button"
						className="ui-link-accent text-xs text-(--accent)"
						data-testid="wiz-back-to-list"
						onClick={() => dispatch({ type: "go", step: 5 })}
					>
						{t(`${K}.backToList`)}
					</button>
				</div>
			)}
			{/* The phone tile is the one most operators are here for, so it takes
			    the full width as the lead; the rest pair up beneath it. */}
			<div
				role="radiogroup"
				aria-labelledby="wiz-kind-title"
				className="grid gap-2 sm:grid-cols-2"
			>
				{KINDS.map((kind) => {
					const selected = state.draft.kind === kind;
					const Icon = KIND_ICON[kind];
					return (
						<label
							key={kind}
							className={`ui-detail-tile ui-wizard-choice flex items-start gap-3 p-3${
								kind === "ntfy" ? " sm:col-span-2" : ""
							}`}
							data-selected={selected ? "true" : "false"}
						>
							<span className="ui-wizard-icon" aria-hidden="true">
								<Icon size={18} />
							</span>
							<span className="min-w-0 flex-1">
								<span className="block text-sm font-semibold text-(--text-primary)">
									{KIND_TITLE[kind]
										? t(`${K}.${KIND_TITLE[kind]}`)
										: t(`settings.alerts.kind.${kind}`)}
								</span>
								<span className="block text-xs text-(--text-muted)">
									{t(`${K}.${KIND_HINT[kind]}`)}
								</span>
							</span>
							<input
								type="radio"
								name="wiz-kind"
								className="mt-1 shrink-0"
								data-testid={`wiz-kind-${kind}`}
								checked={selected}
								onChange={() => dispatch({ type: "setKind", kind, ntfyServer })}
							/>
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

	const discord =
		kind === "discord" ? parseDiscordWebhook(value("webhook")) : null;

	// This exact URL is already on the list the run will finish with, either
	// stored or accepted earlier in this run. The step 3 gate refuses it, so
	// this line explains a Next button that will not move until the destination
	// is changed into a new one.
	const duplicate = isDuplicate(state);

	return (
		<>
			<StepTitle icon={Pencil}>{t(`${K}.step3Title`)}</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step3Hint`)}</p>

			{FIELDS[kind].map((f) => (
				<Fragment key={f.key}>
					<Field
						def={f}
						label={t(`${K}.field.${f.key}`)}
						value={value(f.key)}
						onChange={(v) => set(f.key, v)}
					>
						{kind === "ntfy" && f.key === "topic" && (
							<button
								type="button"
								className="ui-btn ui-btn-secondary shrink-0"
								data-testid="wiz-generate-topic"
								onClick={() => set("topic", generateTopic())}
							>
								{t(`${K}.generate`)}
							</button>
						)}
					</Field>
					{/* The hint belongs under the field it talks about. Nothing here
					    checks that the ntfy server answers: Model Hotel serves
					    `connect-src 'self'`, so a fetch at the operator's own server is
					    blocked before it leaves the page and could only ever report
					    failure. Step 4 sends from the server, which is the side that has
					    to reach the ntfy server anyway. */}
					{kind === "ntfy" && f.key === "server" && (
						<p className="text-xs text-(--text-muted)">
							{t(`${K}.ntfyServerHint`)}
						</p>
					)}
				</Fragment>
			))}

			{/* What to type into the phone, boxed as its own tile: it is the one
			    part of this step that happens somewhere other than this screen. */}
			{kind === "ntfy" && (
				<div className="ui-detail-tile flex items-start gap-3 p-3">
					<span className="ui-wizard-icon" aria-hidden="true">
						<Smartphone size={18} />
					</span>
					<div className="min-w-0 flex-1 space-y-1.5">
						<p className="text-xs text-(--text-secondary)">
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
				</div>
			)}

			{/* Next is gated on a composed URL, so an unparseable webhook otherwise
			    leaves the operator with a dead button and no reason for it. */}
			{kind === "discord" && !discord && value("webhook").trim() !== "" && (
				<p
					data-testid="wiz-discord-error"
					role="status"
					className="text-xs ui-text-error"
				>
					{t(`${K}.discordBad`)}
				</p>
			)}

			{kind === "other" && (
				<a
					className="ui-link-accent text-xs text-(--accent) underline"
					href={APPRISE_SERVICES_URL}
					target="_blank"
					rel="noreferrer"
				>
					{t(`${K}.otherBrowse`)}
				</a>
			)}

			<Composed url={state.draft.url} t={t} />

			{duplicate && (
				<ResultLine ok={false} testId="wiz-already-saved" role="alert">
					{t(`${K}.alreadySaved`)}
				</ResultLine>
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
			<StepTitle icon={Bell}>{t(`${K}.step4Title`)}</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step4Hint`)}</p>
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
				<ResultLine ok={state.testOk} testId="wiz-test-result">
					{state.testOk
						? t(`${K}.testSent.${kind}`)
						: reasonText(state.testError, t, t(`${K}.testFailed`))}
				</ResultLine>
			)}
			{!state.draft.tested && (
				<p className="text-xs text-(--text-muted)">{t(`${K}.testMustPass`)}</p>
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
			<StepTitle icon={ListChecks}>{t(`${K}.step5Title`)}</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step5Hint`)}</p>
			{/* The stored destinations are not this run's work and not this run's to
			    delete, so they are counted rather than listed: the list below is
			    what this run adds, and every row on it can be taken back off. */}
			{savedTargets.length > 0 && (
				<p data-testid="wiz-saved-note" className="text-xs text-(--text-muted)">
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
				<ResultLine ok={rowTest.state === "ok"} testId="wiz-row-test-result">
					{rowTest.state === "ok"
						? t("settings.alerts.testSent")
						: t(`${K}.testFailed`)}
				</ResultLine>
			)}
			<div>
				<button
					type="button"
					className="ui-btn ui-btn-secondary"
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
	managed,
}: StepProps & { catalog: AlertEventDef[]; managed?: boolean }) {
	// Config sync owns the event routing fleet-wide on a managed member, so
	// there is nothing to choose here: the step says who decides instead of
	// offering a selection this member would not be allowed to write.
	if (managed) {
		return (
			<>
				<StepTitle icon={CheckSquare}>{t(`${K}.step6Title`)}</StepTitle>
				<p
					className="ui-callout ui-callout-info"
					data-testid="wiz-managed-events"
				>
					{t(`${K}.managedEventsNote`)}
				</p>
			</>
		);
	}

	return (
		<>
			<StepTitle icon={CheckSquare}>{t(`${K}.step6Title`)}</StepTitle>
			<p className="text-xs text-(--text-muted)">{t(`${K}.step6Hint`)}</p>
			{/* The card's own picker, so the guided run and the card offer the same
			    list in the same order; it reads the catalog from the API itself. */}
			<AlertEventPicker
				value={[...state.events].join(",")}
				onChange={(csv) =>
					dispatch({ type: "resetEvents", types: [...parseCsv(csv)] })
				}
			/>
			{/* An empty selection is a legitimate choice ("set up now, decide what
			    to hear about later"), so it is a note rather than a gate. */}
			{state.events.size === 0 && (
				<p
					data-testid="wiz-none-selected"
					className="ui-callout ui-callout-warning"
				>
					{t(`${K}.noneSelected`)}
				</p>
			)}
			<div>
				<button
					type="button"
					className="ui-link-accent text-xs text-(--accent)"
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
	managed,
	onSendAll,
}: StepProps & {
	/** The destination list the write carries: stored plus this run's work. */
	targets: string[];
	managed?: boolean;
	onSendAll: () => void;
}) {
	if (state.done) {
		return (
			<>
				{/* The run is over, and this tile says so before anything else: a
				    raised tile with the success glyph, the outcome, and the probe
				    read back from the server after the write, so the pill describes
				    the stored configuration rather than the one just typed. */}
				<div
					role="status"
					data-testid="wiz-done"
					className="ui-stat-tile flex items-center gap-3 p-4"
				>
					<CheckCircle2
						size={32}
						className="ui-text-success shrink-0"
						aria-hidden="true"
					/>
					<div className="min-w-0 flex-1 space-y-1">
						<p className="text-base font-semibold text-(--text-primary)">
							{t(`${K}.${managed ? "doneManaged" : "done"}`)}
						</p>
						{state.finalStatus && (
							<p className="flex flex-wrap items-center gap-2 text-xs text-(--text-muted)">
								<span>{t("settings.alerts.apiUrl")}</span>
								<FinalPill status={state.finalStatus} t={t} />
							</p>
						)}
					</div>
				</div>
				<div>
					<button
						type="button"
						className="ui-btn ui-btn-secondary"
						data-testid="wiz-send-all"
						disabled={state.sendingAll}
						onClick={onSendAll}
					>
						{state.sendingAll ? t(`${K}.sending`) : t(`${K}.sendAll`)}
					</button>
				</div>
				{state.sentAll !== "none" && (
					<ResultLine ok={state.sentAll === "ok"} testId="wiz-sent-all">
						{state.sentAll === "ok" ? t(`${K}.sentAll`) : t(`${K}.testFailed`)}
					</ResultLine>
				)}
			</>
		);
	}
	return (
		<>
			<StepTitle icon={CheckCircle2}>{t(`${K}.step7Title`)}</StepTitle>
			{/* A managed member is not switching alerting on and is not choosing
			    events, so the closing line promises only what it writes. */}
			<p className="text-xs text-(--text-muted)">
				{t(`${K}.${managed ? "step7HintManaged" : "step7Hint"}`)}
			</p>
			{/* Trimmed, because the summary promises what the write will store. */}
			<Summary label={t("settings.alerts.apiUrl")}>
				<Mono>{state.apiUrl.trim()}</Mono>
			</Summary>
			{/* One line per destination, so a long list reads as a list. */}
			<Summary
				label={t("settings.alerts.destinations.title")}
				testId="wiz-summary-targets"
			>
				<span className="block space-y-0.5">
					{targets.map((url) => (
						<Mono key={url} block>
							{url}
						</Mono>
					))}
				</span>
			</Summary>
			{/* A managed member writes no event selection, so it promises none.
			    The events are labels, not addresses, so they wrap as chips rather
			    than breaking mid-word the way an address may. */}
			{!managed && (
				<Summary
					label={t("settings.alerts.eventsLabel")}
					testId="wiz-summary-events"
				>
					{state.events.size === 0 ? (
						<span className="text-sm text-(--text-primary)">
							{t(`${K}.noneSelected`)}
						</span>
					) : (
						<span className="flex flex-wrap gap-1.5">
							{[...state.events].map((type) => (
								<span key={type} className="ui-badge ui-badge-accent">
									{eventLabel(t, type)}
								</span>
							))}
						</span>
					)}
				</Summary>
			)}
			{state.finishError !== "" && (
				<ResultLine ok={false} testId="wiz-finish-error" role="alert">
					{state.finishError}
				</ResultLine>
			)}
		</>
	);
}
