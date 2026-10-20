import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	BrainSlashIcon,
	ChevronRight,
	KeyRound,
	RotateCcw,
	ShieldCheck,
} from "@/lib/icons";
import { api } from "../../api/client";
import type { BudgetPeriod, VirtualKey } from "../../api/types";
import { BudgetField } from "../../components/BudgetField";
import { budgetPayload } from "../../components/budget";
import { CopyablePill } from "../../components/CopyablePill";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";
import { intOrNull, numOrNull } from "../../utils/format";
import { ProviderAccessPicker } from "./ProviderAccessPicker";
import { RATE_LIMIT_BOUNDS } from "./rateLimitBounds";
import { UsageSnippets } from "./UsageSnippets";
import { allowedProvidersOf, useProviderCap } from "./useProviderCap";

// The bracketed unit ("(requests/sec)", "（最大同時）") on a line of its own,
// so three narrow tiles side by side wrap at the same place. The space keeps
// the accessible name the whole label.
function breakUnit(label: string) {
	const unit = label.match(/^(.*?)\s*([(（].*)$/);
	if (!unit) return label;
	return (
		<>
			{unit[1]} <br />
			{unit[2]}
		</>
	);
}

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
	const formId = useId();
	const [name, setName] = useState("");
	const [ownerId, setOwnerId] = useState("");
	const [rateLimitRps, setRateLimitRps] = useState<string>("");
	const [rateLimitBurst, setRateLimitBurst] = useState<string>("");
	const [rateLimitTpm, setRateLimitTpm] = useState<string>("");
	const [budgetUsd, setBudgetUsd] = useState<string>("");
	const [budgetPeriod, setBudgetPeriod] = useState<BudgetPeriod>("month");
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
			rate_limit_rps: numOrNull(rateLimitRps),
			rate_limit_burst: intOrNull(rateLimitBurst),
			rate_limit_tpm: intOrNull(rateLimitTpm),
			...budgetPayload(budgetUsd, budgetPeriod),
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
			icon={KeyRound}
			// Not while the create is in flight: a dismissal then lands a key
			// whose secret is never shown (the request still completes).
			closeOnBackdrop={!createdKey && !createMutation.isPending}
			dismissible={!createMutation.isPending}
			onClose={onClose}
			maxWidth="max-w-2xl"
			footer={
				createdKey ? (
					<div className="flex justify-end">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.done")}
						</button>
					</div>
				) : (
					<div className="flex space-x-3 justify-end">
						<button
							type="button"
							onClick={onClose}
							disabled={createMutation.isPending}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="submit"
							form={formId}
							disabled={createMutation.isPending}
							className="ui-btn ui-btn-primary"
						>
							{createMutation.isPending
								? t("common.creating")
								: t("virtualKeys.create.createKey")}
						</button>
					</div>
				)
			}
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
				</>
			) : (
				<form
					id={formId}
					onSubmit={handleSubmit}
					className="grid grid-cols-1 sm:grid-cols-2 gap-2"
				>
					<DetailItem
						label={t("virtualkeys.modal.nameLabel")}
						htmlFor="vk-name"
						className={isAdmin ? undefined : "sm:col-span-2"}
					>
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
					</DetailItem>
					{isAdmin && (
						<DetailItem
							label={t("virtualkeys.modal.form.owner")}
							htmlFor="vk-owner"
						>
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
							<p className="ui-hint mt-1">
								{t("virtualkeys.modal.form.ownerHint")}
							</p>
						</DetailItem>
					)}
					{/* Empty means "inherit the global setting", which is what the
					   placeholder says. */}
					<div className="sm:col-span-2 grid grid-cols-1 sm:grid-cols-3 gap-2">
						{(
							[
								{
									field: "rps",
									label: "virtualkeys.modal.rateLimitRpsLabel",
									tooltip: "virtualkeys.tooltip.rps",
									draft: rateLimitRps,
									setDraft: setRateLimitRps,
								},
								{
									field: "burst",
									label: "virtualkeys.modal.rateLimitBurstLabel",
									tooltip: "virtualkeys.tooltip.burst",
									draft: rateLimitBurst,
									setDraft: setRateLimitBurst,
								},
								{
									field: "tpm",
									label: "virtualkeys.modal.rateLimitTpmLabel",
									tooltip: "virtualkeys.tooltip.tpm",
									draft: rateLimitTpm,
									setDraft: setRateLimitTpm,
								},
							] as const
						).map((l) => (
							<DetailItem
								key={l.field}
								emphasis="stat"
								label={breakUnit(t(l.label))}
								labelExtra={<InfoHint tooltip={t(l.tooltip)} />}
								htmlFor={`vk-rate-limit-${l.field}`}
							>
								<input
									id={`vk-rate-limit-${l.field}`}
									type="number"
									{...RATE_LIMIT_BOUNDS[l.field]}
									value={l.draft}
									onChange={(e) => l.setDraft(e.target.value)}
									className="ui-input"
									placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
								/>
							</DetailItem>
						))}
					</div>
					<DetailItem
						emphasis="stat"
						label={t("budget.label")}
						labelExtra={<InfoHint tooltip={t("budget.tooltip")} />}
						className="sm:col-span-2"
					>
						<BudgetField
							idPrefix="vk-create"
							amount={budgetUsd}
							period={budgetPeriod}
							onAmountChange={setBudgetUsd}
							onPeriodChange={setBudgetPeriod}
							labelClassName="block text-xs text-gray-400 mb-1"
						/>
					</DetailItem>
					<DetailItem
						icon={ShieldCheck}
						label={t("virtualkeys.modal.form.providerAccess")}
						labelExtra={
							cap.excludedProviders.length > 0 ? (
								<button
									type="button"
									onClick={cap.resetProviders}
									className="text-gray-500 hover:text-gray-300 transition-colors"
									aria-label={t("virtualkeys.modal.form.restoreAccess")}
									title={t("virtualkeys.modal.form.restoreAccess")}
								>
									<RotateCcw size={14} />
								</button>
							) : undefined
						}
						className="sm:col-span-2"
					>
						<ProviderAccessPicker
							cap={cap}
							instructionsKey="virtualkeys.modal.providerInstructionsText"
							providerError={providerError}
						/>
					</DetailItem>
					<DetailItem
						icon={BrainSlashIcon}
						label={t("virtualkeys.modal.form.stripReasoning")}
						className="sm:col-span-2"
					>
						<div className="flex items-center gap-3">
							<Toggle
								checked={stripReasoning}
								onChange={setStripReasoning}
								size="sm"
								ariaLabel={t("virtualkeys.modal.form.stripReasoning")}
							/>
							<span className="text-sm text-(--text-primary)">
								{stripReasoning ? t("common.enabled") : t("common.disabled")}
							</span>
						</div>
						<p className="ui-hint mt-1">
							{t("virtualkeys.modal.stripReasoningDescriptionText")}
						</p>
					</DetailItem>
				</form>
			)}
		</Modal>
	);
}
