import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BrainSlashIcon, ChevronRight } from "@/lib/icons";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { CopyablePill } from "../../components/CopyablePill";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";
import { ProviderAccessPicker } from "./ProviderAccessPicker";
import { RateLimitField } from "./RateLimitField";
import { SectionHeader } from "./SectionHeader";
import { UsageSnippets } from "./UsageSnippets";
import { allowedProvidersOf, useProviderCap } from "./useProviderCap";

export function CreateKeyModal({
	onClose,
	onToast,
}: {
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const { t } = useTranslation();
	const { isAdmin } = useIdentity();
	const [name, setName] = useState("");
	const [ownerId, setOwnerId] = useState("");
	const [rateLimitRps, setRateLimitRps] = useState<string>("");
	const [rateLimitBurst, setRateLimitBurst] = useState<string>("");
	const [rateLimitTpm, setRateLimitTpm] = useState<string>("");
	const [stripReasoning, setStripReasoning] = useState(false);
	const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null);
	const [showExamples, setShowExamples] = useState(false);
	const [providerError, setProviderError] = useState("");

	const cap = useProviderCap({
		ownerId,
		capNoteId: "vk-create-provider-cap-note",
	});
	const { sortedProviders, effectiveExcluded, users } = cap;

	const createMutation = useMutation({
		mutationFn: api.virtualKeys.create,
		onSuccess: (vk) => {
			setCreatedKey(vk);
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast(t("virtualkeys.modal.keyCreated"), "success");
		},
		onError: (err: Error) => {
			onToast(
				t("virtualkeys.modal.keyCreatedFailed", { message: err.message }),
				"error",
			);
		},
	});

	const handleSubmit = (e: React.SubmitEvent) => {
		e.preventDefault();
		if (!name.trim()) return;
		setProviderError("");
		const allowedProviders = allowedProvidersOf(
			sortedProviders,
			effectiveExcluded,
		);
		if (allowedProviders && allowedProviders.length === 0) {
			setProviderError(t("virtualKeys.create.providerRequired"));
			return;
		}
		createMutation.mutate({
			name: name.trim(),
			rate_limit_rps: rateLimitRps !== "" ? parseFloat(rateLimitRps) : null,
			rate_limit_burst:
				rateLimitBurst !== "" ? parseInt(rateLimitBurst, 10) : null,
			rate_limit_tpm: rateLimitTpm !== "" ? parseInt(rateLimitTpm, 10) : null,
			allowed_providers: allowedProviders,
			strip_reasoning: stripReasoning,
			owner_user_id: isAdmin && ownerId !== "" ? ownerId : null,
		});
	};

	return (
		<Modal
			title={
				createdKey
					? t("virtualkeys.modal.createdTitle")
					: t("virtualkeys.modal.createTitle")
			}
			closeOnBackdrop={!createdKey}
			onClose={onClose}
			maxWidth={createdKey ? "max-w-2xl" : "max-w-md"}
			scrollable={!!createdKey}
		>
			{createdKey ? (
				<>
					<div className="bg-red-500/10 border-2 border-red-500/40 rounded-[var(--radius-box)] p-3 mb-4">
						<p className="text-red-400 font-semibold text-sm">
							{t("virtualkeys.modal.warningTitle")}
						</p>
						<p className="text-red-400/70 text-xs mt-1">
							{t("virtualkeys.modal.warningText")}
						</p>
					</div>
					<div className="bg-gray-950 rounded-[var(--radius-box)] p-3 mb-4">
						{createdKey.key && (
							<CopyablePill
								text={createdKey.key}
								textClassName="text-sm text-green-400 font-mono break-all"
								tooltip={t("virtualKeys.create.clickToCopyKey")}
							/>
						)}
					</div>
					{createdKey.key && (
						<div className="mb-4">
							<button
								type="button"
								onClick={() => setShowExamples((v) => !v)}
								aria-expanded={showExamples}
								className="ui-link-accent inline-flex items-center gap-1.5 text-sm font-medium"
							>
								<ChevronRight
									size={14}
									className={`transition-transform ${
										showExamples ? "rotate-90" : ""
									}`}
								/>
								{t("virtualkeys.modal.usageExamples")}
							</button>
							{showExamples && (
								<div className="mt-3">
									<UsageSnippets apiKey={createdKey.key} />
								</div>
							)}
						</div>
					)}
					<div className="flex justify-end">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.done")}
						</button>
					</div>
				</>
			) : (
				<form onSubmit={handleSubmit} className="space-y-4">
					<div>
						<label
							htmlFor="vk-name"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("virtualkeys.modal.nameLabel")}
						</label>
						<input
							id="vk-name"
							type="text"
							required
							maxLength={100}
							value={name}
							onChange={(e) => setName(e.target.value)}
							className="ui-input"
							placeholder={t("virtualkeys.modal.form.namePlaceholder")}
						/>
					</div>
					{isAdmin && (
						<div>
							<label
								htmlFor="vk-owner"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								{t("virtualkeys.modal.form.owner")}
							</label>
							<select
								id="vk-owner"
								value={ownerId}
								onChange={(e) => setOwnerId(e.target.value)}
								className="ui-input"
								data-testid="vk-owner-select"
							>
								<option value="">
									{t("virtualkeys.modal.form.ownerNone")}
								</option>
								{(users ?? []).map((u) => (
									<option key={u.id} value={u.id}>
										{u.username}
									</option>
								))}
							</select>
							<p className="text-xs text-gray-500 mt-1">
								{t("virtualkeys.modal.form.ownerHint")}
							</p>
						</div>
					)}
					<RateLimitField
						id="vk-rate-limit-rps"
						labelKey="virtualkeys.modal.rateLimitRpsLabel"
						field="rps"
						value={rateLimitRps}
						onChange={setRateLimitRps}
					/>
					<RateLimitField
						id="vk-rate-limit-burst"
						labelKey="virtualkeys.modal.rateLimitBurstLabel"
						field="burst"
						value={rateLimitBurst}
						onChange={setRateLimitBurst}
					/>
					<RateLimitField
						id="vk-rate-limit-tpm"
						labelKey="virtualkeys.modal.rateLimitTpmLabel"
						field="tpm"
						value={rateLimitTpm}
						onChange={setRateLimitTpm}
					/>
					<ProviderAccessPicker
						cap={cap}
						headingKey="virtualkeys.modal.form.providerAccess"
						instructionsKey="virtualkeys.modal.providerInstructionsText"
						providerError={providerError}
					/>

					<div>
						<SectionHeader
							icon={BrainSlashIcon}
							label={t("virtualkeys.modal.form.stripReasoning")}
							spacing="mb-2"
						/>
						<div className="flex items-center gap-3">
							<Toggle
								checked={stripReasoning}
								onChange={setStripReasoning}
								size="sm"
								ariaLabel={t("virtualkeys.modal.form.stripReasoning")}
							/>
							<span className="text-sm text-gray-200">
								{stripReasoning ? t("common.enabled") : t("common.disabled")}
							</span>
						</div>
						<p className="text-xs text-gray-400 mt-1.5">
							{t("virtualkeys.modal.stripReasoningDescriptionText")}
						</p>
					</div>

					<div className="flex space-x-3 justify-end pt-2">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="submit"
							disabled={createMutation.isPending}
							className="ui-btn ui-btn-primary"
						>
							{createMutation.isPending
								? t("common.creating")
								: t("virtualKeys.create.createKey")}
						</button>
					</div>
				</form>
			)}
		</Modal>
	);
}
