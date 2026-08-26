import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { Spinner } from "../../components/Spinner";

/**
 * The management footer: enable/disable, test, delete (two-step) on the left;
 * edit / save-cancel and re-discover with its cooldown on the right.
 */
export function ModelActionsFooter({
	model,
	editing,
	testing,
	testError,
	cooldown,
	discovering,
	confirmDelete,
	onToggle,
	onTest,
	onArmDelete,
	onDelete,
	onEdit,
	onCancelEdit,
	onSave,
	onDiscover,
}: {
	model: Model;
	editing: boolean;
	testing: boolean;
	testError: boolean;
	cooldown: number;
	discovering: boolean;
	confirmDelete: boolean;
	onToggle: () => void;
	onTest: () => void;
	onArmDelete: () => void;
	onDelete: () => void;
	onEdit: () => void;
	onCancelEdit: () => void;
	onSave: () => void;
	onDiscover: () => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center justify-between mt-4 pt-4">
			<div className="flex items-center gap-2">
				<button
					type="button"
					onClick={onToggle}
					className={`ui-btn ${model.enabled ? "ui-btn-primary" : "ui-btn-danger"}`}
				>
					{model.enabled ? t("common.enabled") : t("common.disabled")}
				</button>
				<button
					type="button"
					disabled={testing}
					onClick={onTest}
					className={`ui-btn ${testError ? "ui-btn-danger" : "ui-btn-secondary"}`}
				>
					{testing && <Spinner />}
					{testing ? t("models.detail.testing") : t("models.detail.test")}
				</button>
				{!confirmDelete ? (
					<button
						type="button"
						onClick={onArmDelete}
						className="ui-btn ui-btn-danger-muted"
					>
						{t("common.delete")}
					</button>
				) : (
					<button
						type="button"
						onClick={onDelete}
						className="ui-btn ui-btn-danger"
					>
						{t("models.detail.confirmDelete")}
					</button>
				)}
			</div>
			<div className="flex items-center gap-2">
				{editing ? (
					<>
						<button
							type="button"
							onClick={onCancelEdit}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="button"
							onClick={onSave}
							className="ui-btn ui-btn-primary"
						>
							{t("common.saveChanges")}
						</button>
					</>
				) : (
					<>
						<button
							type="button"
							onClick={onEdit}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.edit")}
						</button>
						<button
							type="button"
							disabled={cooldown > 0 || discovering}
							onClick={onDiscover}
							className="ui-btn bg-(--accent-light) text-(--accent) hover:brightness-125"
						>
							{discovering ? (
								<>
									<Spinner /> {t("models.detail.updating")}
								</>
							) : cooldown > 0 ? (
								t("models.detail.updateCooldown", { cooldown })
							) : (
								t("models.detail.updateInfo")
							)}
						</button>
					</>
				)}
			</div>
		</div>
	);
}
