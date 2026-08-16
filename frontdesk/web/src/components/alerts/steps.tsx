import type { TFunction } from "i18next";
import { type Dispatch, type ReactNode, useState } from "react";
import { generateTopic } from "../../utils/ntfy";
import type { Action, WizardState } from "./AlertsWizard";
import {
	type DestinationKind,
	FIELDS,
	type FieldDef,
	parseUnifiedPushEndpoint,
} from "./composers";

// The four step bodies the alerts wizard shows before its destination list:
// prove apprise-api answers, pick what kind of destination this is, fill in the
// parts that kind needs, and deliver one real test to it. They are presentation
// only; every gate lives in AlertsWizard's canNext, so a step can never let
// itself through.

const K = "settings.alerts.wizard";
const APPRISE_SERVICES_URL = "https://AppriseIt.com/services/";

export interface StepProps {
	state: WizardState;
	dispatch: Dispatch<Action>;
	t: TFunction;
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
			<h3 className="fd-step-title">{t(`${K}.step1Title`)}</h3>
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
			{status && (
				<p
					data-testid="wiz-api-status"
					data-ok={status.healthy ? "true" : "false"}
					className={status.healthy ? "fd-faint" : "fd-error-text"}
				>
					{status.healthy
						? t(`${K}.apiOk`)
						: reasonText(status.reason ?? "", t)}
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

export function StepKind({
	state,
	dispatch,
	t,
	ntfyServer,
}: StepProps & { ntfyServer: string }) {
	return (
		<>
			<h3 className="fd-step-title">{t(`${K}.step2Title`)}</h3>
			<p className="fd-faint fd-step-intro">{t(`${K}.step2Hint`)}</p>
			<div className="fd-stack" style={{ gap: "0.4rem" }}>
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
									{t(`settings.alerts.kind.${kind}`)}
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
	const [softCheck, setSoftCheck] = useState<"none" | "ok" | "unknown">("none");
	if (kind === null) return null;

	const value = (key: string) => state.draft.fields[key] ?? "";
	const set = (key: string, v: string) =>
		dispatch({ type: "setField", key, value: v });

	// A courtesy reachability ping at the server the operator typed, straight from
	// the browser. It is only ever a hint: private servers, CORS and captive
	// networks all make it fail for reasons that say nothing about whether Front
	// Desk can deliver, which is what the next step actually proves.
	const probeNtfyServer = () => {
		const server = value("server").trim().replace(/\/+$/, "");
		if (server === "") {
			setSoftCheck("none");
			return;
		}
		fetch(`${server}/v1/health`, { signal: AbortSignal.timeout(3000) })
			.then((res) => setSoftCheck(res.ok ? "ok" : "unknown"))
			.catch(() => setSoftCheck("unknown"));
	};

	const parsed =
		kind === "bellhop" ? parseUnifiedPushEndpoint(value("endpoint")) : null;

	return (
		<>
			<h3 className="fd-step-title">{t(`${K}.step3Title`)}</h3>
			<p className="fd-faint fd-step-intro">{t(`${K}.step3Hint`)}</p>

			{FIELDS[kind].map((f) => (
				<Field
					key={f.key}
					def={f}
					label={t(`${K}.field.${f.key}`)}
					value={value(f.key)}
					onChange={(v) => set(f.key, v)}
					onBlur={
						kind === "ntfy" && f.key === "server" ? probeNtfyServer : undefined
					}
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

			{kind === "ntfy" && (
				<>
					<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
						{t(`${K}.ntfyServerHint`)}
					</p>
					{softCheck !== "none" && (
						<p
							data-testid="wiz-ntfy-soft-check"
							data-ok={softCheck === "ok" ? "true" : "false"}
							className="fd-faint"
							style={{ fontSize: "0.82rem" }}
						>
							{softCheck === "ok"
								? t(`${K}.ntfySoftOk`)
								: t(`${K}.ntfySoftUnknown`)}
						</p>
					)}
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
						<p data-testid="wiz-bellhop-error" className="fd-error-text">
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
			<h3 className="fd-step-title">{t(`${K}.step4Title`)}</h3>
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
					data-testid="wiz-test-result"
					data-ok={state.testOk ? "true" : "false"}
					className={state.testOk ? "fd-faint" : "fd-error-text"}
				>
					{state.testOk
						? t(`${K}.testSent.${kind}`)
						: reasonText(state.testError, t)}
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

// reasonText renders a server reason code. Codes the catalog does not cover
// (and a failure that carried none) fall back to the generic wording rather
// than leaking a raw key into the dialog.
function reasonText(code: string, t: TFunction): string {
	return t(`settings.alerts.reason.${code}`, {
		defaultValue: t("settings.alerts.testFailed"),
	});
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
	onBlur,
	children,
}: {
	def: FieldDef;
	label: string;
	value: string;
	onChange: (v: string) => void;
	onBlur?: () => void;
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
					onBlur={onBlur}
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
	if (value === "") return null;
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
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
