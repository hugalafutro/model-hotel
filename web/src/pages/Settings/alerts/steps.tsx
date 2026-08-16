import type { TFunction } from "i18next";
import {
	type Dispatch,
	type ReactNode,
	useEffect,
	useRef,
	useState,
} from "react";
import { generateTopic } from "../../../utils/ntfy";
import { type Action, isDuplicate, type WizardState } from "./AlertsWizard";
import {
	APPRISE_SERVICES_URL,
	type DestinationKind,
	FIELDS,
	type FieldDef,
	parseDiscordWebhook,
} from "./composers";

// The first four step bodies of the alerts wizard: prove apprise-api answers,
// pick what kind of destination this is, fill in the parts that kind needs, and
// deliver one real test to it. They are presentation only; every gate lives in
// AlertsWizard's canNext, so a step can never let itself through, and none of
// them persists anything.

const K = "settings.alerts.wizard";

export interface StepProps {
	state: WizardState;
	dispatch: Dispatch<Action>;
	t: TFunction;
}

// StepTitle names the step, and is the only part of the wizard that announces
// itself. The live region is deliberately this small: over the whole step body
// it would read every keystroke in the destination fields back at the operator,
// while over the title alone it says which step the run just moved to. The
// wrapper carries the role so the heading stays a heading, and the role="alert"
// nodes inside the body (a failed test) are outside this region and keep
// announcing themselves.
function StepTitle({ id, children }: { id?: string; children: ReactNode }) {
	return (
		<div role="status">
			<h3 className="text-base font-semibold text-(--text-primary)" id={id}>
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
					className="ui-callout ui-callout-warning text-xs"
				>
					{t(`${K}.apiChangedDrops`)}
				</p>
			)}
			{status && (
				<p
					role="status"
					data-testid="wiz-api-status"
					data-ok={status.healthy ? "true" : "false"}
					className={`text-sm ${status.healthy ? "ui-text-success" : "ui-text-error"}`}
				>
					{status.healthy
						? t(`${K}.apiOk`)
						: reasonText(status.reason ?? "", t, t(`${K}.apiFailed`))}
				</p>
			)}
		</>
	);
}

// Model Hotel has no companion app of its own, so the phone tile is ntfy's.
// (composers.ts still recognises a Bellhop topic when it describes a stored
// destination that Front Desk set up.)
const KINDS: DestinationKind[] = [
	"ntfy",
	"telegram",
	"discord",
	"email",
	"other",
];

const KIND_HINT: Record<string, string> = {
	ntfy: "kindNtfyHint",
	telegram: "kindTelegramHint",
	discord: "kindDiscordHint",
	email: "kindEmailHint",
	other: "kindOtherHint",
};

// Three tiles are named after the service itself, so they reuse the shared
// settings.alerts.kind.* labels. Two need more than the bare name to be picked
// correctly: "ntfy" means nothing until it says it is the phone app, and
// "Apprise URL" is the catch-all rather than a service.
const KIND_TITLE: Record<string, string> = {
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
			<p className="text-xs text-(--text-muted)">{t(`${K}.step2Hint`)}</p>
			{/* "Add another" is one click, so undoing it has to be one click too:
			    Back walks the run's own order (towards the apprise address), which
			    is not where a second destination was started from. */}
			{state.added.length > 0 && (
				<div>
					<button
						type="button"
						className="ui-link-accent text-xs"
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
				className="space-y-2"
			>
				{KINDS.map((kind) => {
					const selected = state.draft.kind === kind;
					return (
						<label
							key={kind}
							className="ui-card flex items-start gap-3 p-3 cursor-pointer"
							// The selected tile is drawn in the theme's accent. An inline
							// style rather than a utility class: index.css is unlayered, so
							// .ui-card's own border rule outranks any Tailwind border a
							// checked-state variant could apply.
							style={{ borderColor: selected ? "var(--accent)" : undefined }}
						>
							<input
								type="radio"
								name="wiz-kind"
								className="mt-1"
								data-testid={`wiz-kind-${kind}`}
								checked={selected}
								onChange={() => dispatch({ type: "setKind", kind, ntfyServer })}
							/>
							<span>
								<span className="block text-sm font-semibold text-(--text-primary)">
									{KIND_TITLE[kind]
										? t(`${K}.${KIND_TITLE[kind]}`)
										: t(`settings.alerts.kind.${kind}`)}
								</span>
								<span className="block text-xs text-(--text-muted)">
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
			<p className="text-xs text-(--text-muted)">{t(`${K}.step3Hint`)}</p>

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
							className="ui-btn ui-btn-secondary shrink-0"
							data-testid="wiz-generate-topic"
							onClick={() => set("topic", generateTopic())}
						>
							{t(`${K}.generate`)}
						</button>
					)}
				</Field>
			))}

			{/* Nothing here checks that the ntfy server answers: Model Hotel serves
			    `connect-src 'self'`, so a fetch at the operator's own server is
			    blocked before it leaves the page and could only ever report
			    failure. Step 4 sends from the server, which is the side that has to
			    reach the ntfy server anyway. */}
			{kind === "ntfy" && (
				<>
					<p className="text-xs text-(--text-muted)">
						{t(`${K}.ntfyServerHint`)}
					</p>
					<div className="space-y-1">
						<p className="text-xs text-(--text-muted)">
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
					className="ui-link-accent text-xs"
					href={APPRISE_SERVICES_URL}
					target="_blank"
					rel="noreferrer"
				>
					{t(`${K}.otherBrowse`)}
				</a>
			)}

			<Composed url={state.draft.url} t={t} />

			{duplicate && (
				<p
					data-testid="wiz-already-saved"
					role="alert"
					className="text-xs ui-text-error"
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
				<p
					role="status"
					data-testid="wiz-test-result"
					data-ok={state.testOk ? "true" : "false"}
					className={`text-sm ${state.testOk ? "ui-text-success" : "ui-text-error"}`}
				>
					{state.testOk
						? t(`${K}.testSent.${kind}`)
						: reasonText(state.testError, t, t(`${K}.testFailed`))}
				</p>
			)}
			{!state.draft.tested && (
				<p className="text-xs text-(--text-muted)">{t(`${K}.testMustPass`)}</p>
			)}
		</>
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
		<div className="space-y-1">
			<span className="block text-sm font-medium text-(--text-secondary)">
				{t(`${K}.composedLabel`)}
			</span>
			{/* The theme's own mono face: Tailwind's font-mono is a fixed stack and
			    would ignore the Terminal style's JetBrains Mono. */}
			<code
				className="block text-xs text-(--text-primary) select-all break-all"
				data-testid="wiz-composed"
				style={{ fontFamily: "var(--font-mono)" }}
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
		<div className="space-y-1.5">
			<label
				className="text-sm font-medium text-(--text-secondary)"
				htmlFor={id}
			>
				{label}
			</label>
			<div className="flex items-center gap-2">
				<input
					id={id}
					data-testid={id}
					className="ui-input text-sm w-full"
					// Secrets are hidden while they are typed, which is the only moment
					// someone could be looking over a shoulder; nothing is masked after.
					type={def.secret ? "password" : "text"}
					placeholder={def.placeholder}
					spellCheck={false}
					autoComplete="off"
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
	if (value === "") return null;
	return (
		<div className="flex items-center gap-2 flex-wrap">
			<span className="text-xs text-(--text-muted)">{label}</span>
			<code
				className="text-xs text-(--text-primary) select-all break-all"
				style={{ fontFamily: "var(--font-mono)" }}
			>
				{value}
			</code>
			<button
				type="button"
				className="ui-btn ui-btn-secondary"
				data-testid={testId}
				aria-label={`${t("common.copy")}: ${label}`}
				onClick={copy}
			>
				{copied ? t("common.copied") : t("common.copy")}
			</button>
		</div>
	);
}
