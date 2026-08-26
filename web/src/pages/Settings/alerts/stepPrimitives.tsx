import type { FieldDef } from "@web-shared/alerts/composers";
import type { TFunction } from "i18next";
import type { ReactNode } from "react";
import { CheckCircle2, type LucideIcon, XCircle } from "@/lib/icons";
import type { AlertStatus } from "../../../api/types";
import { CopyButton } from "../../../components/CopyButton";
import { K } from "./stepShared";

// StepTitle names the step. It is a plain heading: the step change is announced
// by the wizard's own live region (AlertsWizard's StepAnnouncer), which is
// mounted once for the whole run so a step change mutates its text rather than
// inserting a new region, and screen readers reliably miss the latter. The icon
// chip beside the title is the same accent chip the settings sections and
// dashboard cards wear, so each step opens like a section of the app rather
// than a form field.
export function StepTitle({
	id,
	icon: Icon,
	children,
}: {
	id?: string;
	icon: LucideIcon;
	children: ReactNode;
}) {
	return (
		<div className="flex items-center gap-3">
			<span className="ui-wizard-icon" aria-hidden="true">
				<Icon size={18} />
			</span>
			<h3 className="text-base font-semibold text-(--text-primary)" id={id}>
				{children}
			</h3>
		</div>
	);
}
// ResultLine is one sentence reporting how something the operator just asked
// for turned out, in the theme's success or error tone with the matching
// glyph. The glyph is decoration: the sentence carries the meaning, and the
// element's text is the sentence alone.
export function ResultLine({
	ok,
	testId,
	role = "status",
	children,
}: {
	ok: boolean;
	testId: string;
	role?: "status" | "alert";
	children: ReactNode;
}) {
	const Icon = ok ? CheckCircle2 : XCircle;
	return (
		<p
			role={role}
			data-testid={testId}
			data-ok={ok ? "true" : "false"}
			className={`flex items-start gap-1.5 text-sm ${ok ? "ui-text-success" : "ui-text-error"}`}
		>
			<Icon size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
			<span>{children}</span>
		</p>
	);
}
// FinalPill reports the probe taken straight after the write. The card's own
// status line covers a fourth state (nothing configured at all) that cannot
// happen here: the wizard has just configured it.
export function FinalPill({
	status,
	t,
}: {
	status: AlertStatus;
	t: TFunction;
}) {
	const [variant, label] = !status.reachable
		? (["ui-badge-error", "unreachable"] as const)
		: !status.healthy
			? (["ui-badge-warning", "issues"] as const)
			: (["ui-badge-success", "reachable"] as const);
	return (
		<span
			className={`ui-badge ${variant}`}
			data-testid="wiz-done-pill"
			title={status.detail}
		>
			{t(`settings.alerts.status.${label}`)}
		</span>
	);
}
// One labelled tile of the closing summary: what is about to be written, in
// the operator's own words rather than as the settings keys it becomes.
export function Summary({
	label,
	testId,
	children,
}: {
	label: string;
	testId?: string;
	children: ReactNode;
}) {
	return (
		<div className="ui-detail-tile space-y-1.5 px-3 py-2.5">
			<span className="block text-[11px] font-medium uppercase tracking-wider text-(--text-tertiary)">
				{label}
			</span>
			<span className="block min-w-0" data-testid={testId}>
				{children}
			</span>
		</div>
	);
}
// Mono is an address in the theme's own mono face (Tailwind's font-mono is a
// fixed stack and would ignore the Terminal style's JetBrains Mono). It may
// break anywhere: an address has no word boundaries worth keeping.
export function Mono({
	block,
	children,
}: {
	block?: boolean;
	children: ReactNode;
}) {
	return (
		<code
			className={`${block ? "block " : ""}text-xs text-(--text-primary) select-all break-all`}
			style={{ fontFamily: "var(--font-mono)" }}
		>
			{children}
		</code>
	);
}
// The composed Apprise URL, shown from the moment the fields make a valid one.
// It is the thing that gets tested and stored, so the operator sees it rather
// than having to trust that the fields were assembled the way they expect.
export function Composed({ url, t }: { url: string; t: TFunction }) {
	if (url === "") return null;
	return (
		<div className="ui-detail-tile space-y-1.5 px-3 py-2.5">
			<span className="block text-[11px] font-medium uppercase tracking-wider text-(--text-tertiary)">
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
export function Field({
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
export function CopyRow({
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
			<CopyButton
				variant="label"
				text={value}
				testId={testId}
				ariaLabel={`${t("common.copy")}: ${label}`}
			/>
		</div>
	);
}
