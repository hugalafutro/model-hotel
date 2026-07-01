import { useEffect, useState } from "react";
import { Eye, EyeOff, Trash2 } from "@/lib/icons";
import { ConfirmDialog } from "./ConfirmDialog";

interface SecretFieldProps {
	/** id for the input, so an external <label htmlFor> binds to it. */
	id: string;
	/** Prefix for data-testids: `${testId}-input/-reveal/-clear/-confirm`. */
	testId: string;
	/** The draft value being entered (never the stored secret, which is masked). */
	value: string;
	/** Whether a secret is already saved (controls the clear control). */
	configured: boolean;
	placeholder: string;
	onChange: (v: string) => void;
	/** Commit the draft (blur / Enter). */
	onCommit: () => void;
	/** Clear the stored secret. */
	onClear: () => void;
	toggleLabel: string;
	clearLabel: string;
	clearConfirmTitle: string;
	clearConfirmMessage: string;
}

/**
 * SecretField is the encrypted-secret input shared by the SSO panels (OIDC,
 * GitHub). The stored secret is masked on read and never echoed back, so:
 *   - The reveal eye only appears while a draft is being entered, letting the
 *     operator confirm exactly what they pasted (e.g. a stray trailing space).
 *     With nothing entered there is nothing to reveal, so the eye is hidden.
 *   - Clearing the stored secret is destructive (it must be pasted again), so it
 *     is an icon gated behind a confirm dialog, matching the passkey-delete flow.
 */
export function SecretField({
	id,
	testId,
	value,
	configured,
	placeholder,
	onChange,
	onCommit,
	onClear,
	toggleLabel,
	clearLabel,
	clearConfirmTitle,
	clearConfirmMessage,
}: SecretFieldProps) {
	const [showSecret, setShowSecret] = useState(false);
	const [confirmClear, setConfirmClear] = useState(false);

	const hasDraft = value.length > 0;

	// The draft is committed and reset to "" by the parent. Re-mask on reset so a
	// later edit never starts unexpectedly revealed.
	useEffect(() => {
		if (!hasDraft) setShowSecret(false);
	}, [hasDraft]);

	return (
		<div className="flex items-center gap-2">
			<input
				id={id}
				type={showSecret && hasDraft ? "text" : "password"}
				value={value}
				placeholder={placeholder}
				spellCheck={false}
				autoComplete="off"
				onChange={(e) => onChange(e.target.value)}
				onBlur={onCommit}
				onKeyDown={(e) => {
					if (e.key === "Enter") e.currentTarget.blur();
				}}
				className="ui-input text-sm w-full font-mono"
				data-testid={`${testId}-input`}
			/>
			{hasDraft && (
				<button
					type="button"
					className="ui-icon-btn p-1.5"
					// preventDefault keeps focus in the input: without it the click
					// blurs the field, firing onCommit (which saves and clears the
					// draft) before the reveal can be seen.
					onMouseDown={(e) => e.preventDefault()}
					onClick={() => setShowSecret((v) => !v)}
					aria-label={toggleLabel}
					aria-pressed={showSecret}
					title={toggleLabel}
					data-testid={`${testId}-reveal`}
				>
					{showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
				</button>
			)}
			{configured && (
				<button
					type="button"
					className="ui-icon-btn p-1.5 hover:text-red-400"
					// Same focus guard as the reveal: don't blur-commit a pending draft
					// just because the operator reached for the clear control.
					onMouseDown={(e) => e.preventDefault()}
					onClick={() => setConfirmClear(true)}
					aria-label={clearLabel}
					title={clearLabel}
					data-testid={`${testId}-clear`}
				>
					<Trash2 size={16} />
				</button>
			)}
			{confirmClear && (
				<ConfirmDialog
					title={clearConfirmTitle}
					message={clearConfirmMessage}
					fields={[]}
					confirmLabel={clearLabel}
					confirmTestId={`${testId}-confirm`}
					onConfirm={() => {
						setConfirmClear(false);
						onClear();
					}}
					onCancel={() => setConfirmClear(false)}
				/>
			)}
		</div>
	);
}
