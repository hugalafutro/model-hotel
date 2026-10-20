import { useTranslation } from "react-i18next";
import type { ProviderCap } from "./useProviderCap";

/**
 * The provider-access chip cloud shared by the create and the edit form: the
 * cap note the inert chips point at, and one chip per provider that strikes
 * through when excluded. The surrounding tile names the section and draws the
 * restore button.
 */
export function ProviderAccessPicker({
	cap,
	instructionsKey,
	providerError,
}: {
	cap: ProviderCap;
	instructionsKey: string;
	providerError: string;
}) {
	const { t } = useTranslation();
	const {
		sortedProviders,
		effectiveExcluded,
		outsideCapIds,
		isOutsideCap,
		capIsOtherOwner,
		capNote,
		capNoteId,
		toggleProvider,
	} = cap;

	return (
		<>
			<div>
				<p className="ui-hint mb-2">{t(instructionsKey)}</p>
				{outsideCapIds.length > 0 && (
					<p
						id={capNoteId}
						data-testid="vk-provider-cap-note"
						data-cap-source={capIsOtherOwner ? "owner" : "account"}
						className="text-xs text-gray-500 italic mb-2"
					>
						{capNote}
					</p>
				)}
				{sortedProviders.length === 0 ? (
					<p className="text-xs text-gray-500 italic">
						{t("virtualkeys.modal.form.noProviders")}
					</p>
				) : (
					<div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto">
						{sortedProviders.map((provider) => {
							const outsideCap = isOutsideCap(provider.id);
							const isExcluded = effectiveExcluded.includes(provider.id);
							return (
								<button
									key={provider.id}
									type="button"
									data-testid={`vk-provider-option-${provider.id}`}
									// aria-disabled, not disabled: a disabled button drops out of
									// the tab order, which would put both the title and the note
									// out of reach of a keyboard or a screen reader.
									// toggleProvider is what makes it inert.
									{...(outsideCap
										? {
												"data-outside-cap": "true",
												"aria-disabled": true,
												"aria-describedby": capNoteId,
											}
										: {})}
									title={outsideCap ? capNote : undefined}
									onClick={() => toggleProvider(provider.id)}
									aria-pressed={isExcluded}
									className={`inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium transition-colors ui-badge
												${
													isExcluded
														? "ui-badge-neutral line-through opacity-60"
														: "ui-badge-accent"
												}
												${outsideCap ? "cursor-not-allowed" : isExcluded ? "hover:brightness-125" : ""}`}
								>
									{provider.name}
								</button>
							);
						})}
					</div>
				)}
			</div>
			{providerError && (
				<p className="text-xs text-red-400 mt-1">{providerError}</p>
			)}
		</>
	);
}
