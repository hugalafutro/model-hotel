import { useState } from "react";

/**
 * A text input that reports its value on blur (or Enter), not per keystroke, so
 * a settings field does not fire a save for every character typed. While the
 * user is typing the draft is shown; once committed the input follows `value`
 * again, so a rejected save snaps back to what the server holds.
 */
export function BlurCommitInput({
	id,
	value,
	onCommit,
	className,
	placeholder,
	mono = false,
	testId,
	disabled,
}: {
	id: string;
	value: string;
	/** Called with the new text, only when it differs from `value`. */
	onCommit: (next: string) => void;
	className?: string;
	placeholder?: string;
	/** True for a monospace field (ids, keys, hostnames). */
	mono?: boolean;
	testId?: string;
	disabled?: boolean;
}) {
	const [draft, setDraft] = useState<string | null>(null);
	return (
		<input
			id={id}
			type="text"
			value={draft ?? value}
			placeholder={placeholder}
			spellCheck={false}
			autoComplete="off"
			disabled={disabled}
			onChange={(e) => setDraft(e.target.value)}
			onBlur={() => {
				if (draft !== null && draft !== value) onCommit(draft);
				setDraft(null);
			}}
			onKeyDown={(e) => {
				if (e.key === "Enter") e.currentTarget.blur();
			}}
			className={
				className ?? `ui-input text-sm w-full${mono ? " font-mono" : ""}`
			}
			data-testid={testId}
		/>
	);
}
