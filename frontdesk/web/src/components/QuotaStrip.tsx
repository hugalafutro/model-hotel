import {
	ArrowClockwiseIcon,
	CaretDownIcon,
	CaretUpIcon,
} from "@phosphor-icons/react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";
import { useToast } from "../context/ToastContext";
import { useQuota } from "../hooks/useQuota";
import {
	payloadOf,
	type QuotaBadgeModel,
	type QuotaBarMode,
	toBadgeModels,
} from "../utils/quota";
import { formatTimeOfDay } from "../utils/time";
import { ErrorBoundary } from "./ErrorBoundary";
import { QuotaBadge } from "./QuotaBadge";
import { KimiCodeQuotaModal } from "./quota/KimiCodeQuotaModal";
import { MiniMaxQuotaModal } from "./quota/MiniMaxQuotaModal";
import { NanoGPTQuotaModal } from "./quota/NanoGPTQuotaModal";
import { NeuralWattQuotaModal } from "./quota/NeuralWattQuotaModal";
import { OpenRouterQuotaModal } from "./quota/OpenRouterQuotaModal";
import { ZAICodingQuotaModal } from "./quota/ZAICodingQuotaModal";

const COLLAPSED_KEY = "fdQuotaCollapsed";
const BAR_MODE_KEY = "fdQuotaBarMode";

/** Providers whose badge says everything the modal would: click means refresh. */
const NO_MODAL = new Set(["deepseek", "ollama-cloud"]);

function readCollapsed(): boolean {
	try {
		return localStorage.getItem(COLLAPSED_KEY) === "true";
	} catch {
		return false;
	}
}

function readBarMode(): QuotaBarMode {
	try {
		return localStorage.getItem(BAR_MODE_KEY) === "used" ? "used" : "remaining";
	} catch {
		return "remaining";
	}
}

function store(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		/* private mode: the preference just does not survive the reload */
	}
}

/**
 * The quota strip under the header, on every tab.
 *
 * Unlike the Model Hotel sidebar, badges and modals are siblings in one subtree,
 * so bar mode is ordinary state passed down rather than a localStorage plus
 * custom-event bridge. It is still persisted, purely so it survives a reload.
 */
export function QuotaStrip() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [collapsed, setCollapsed] = useState(readCollapsed);
	const [barMode, setBarMode] = useState<QuotaBarMode>(readBarMode);
	const [openKey, setOpenKey] = useState<string | null>(null);

	const { snapshots, loading, stale, lastUpdatedAt, refreshing, refresh } =
		useQuota(collapsed);
	const models = toBadgeModels(snapshots);
	const open = openKey ? models.find((m) => m.key === openKey) : undefined;

	// M-4: a provider that drops out of a genuinely loaded list must not leave
	// its modal's key sitting in state, or the badge reappearing on a later
	// poll would silently reopen a dialog the operator never asked to see
	// again. Corrected directly in the render body (React's documented
	// "adjusting state" bail-out-and-re-render pattern), not in a useEffect:
	// an effect would commit the stale modal to the screen for one frame
	// before closing it, and synchronous setState-in-effect is exactly what
	// react-hooks/set-state-in-effect exists to catch. Gated on `!loading`
	// (at least one read has completed) rather than firing on any transient
	// state: useQuota only ever replaces `snapshots` atomically on a
	// SUCCESSFUL read (a failed read keeps the last-good cache untouched, see
	// useQuota's `read`), so this can only ever trigger on a genuine change
	// to the fleet primary's export, never on an in-flight or failed fetch.
	// It cannot loop: the condition requires `openKey` to be non-null, and
	// this sets it to null, so it cannot re-fire on the render it causes.
	if (!loading && openKey && !open) {
		setOpenKey(null);
	}

	const doRefresh = useCallback(async () => {
		const outcome = await refresh();
		if (outcome === "cooldown") toast(t("quota.refreshCooldown"), "info");
		else if (outcome === "failed") toast(t("quota.refreshFailed"), "error");
		else toast(t("quota.refreshed"), "success");
	}, [refresh, toast, t]);

	const toggleCollapsed = useCallback(() => {
		setCollapsed((prev) => {
			const next = !prev;
			store(COLLAPSED_KEY, String(next));
			// M-3: collapsing hides the badges but previously left an open modal
			// floating over the now-empty strip. Close it along with the badges;
			// idempotent, so a StrictMode double-invoke of this updater is
			// harmless. Deliberately one-directional: EXPANDING later must not
			// resurrect a modal the operator dismissed by collapsing.
			if (next) setOpenKey(null);
			return next;
		});
	}, []);

	const toggleBarMode = useCallback(() => {
		setBarMode((prev) => {
			const next: QuotaBarMode = prev === "remaining" ? "used" : "remaining";
			store(BAR_MODE_KEY, next);
			return next;
		});
	}, []);

	const onBadgeClick = useCallback(
		(model: QuotaBadgeModel) => {
			// A degraded badge has no payload to show, and two providers have no
			// modal at all, so in both cases the useful action is a refresh.
			if (model.degraded || NO_MODAL.has(model.type)) {
				void doRefresh();
				return;
			}
			setOpenKey(model.key);
		},
		[doRefresh],
	);

	// Nothing known and nothing cached: stay out of the way rather than parking a
	// permanent empty bar above every tab. An unreachable primary is already
	// reported prominently on the Members page.
	if (models.length === 0) return null;

	return (
		<div className="fd-quota-strip" data-testid="quota-strip">
			<div className="fd-quota-strip-head">
				<span className="fd-quota-strip-label">{t("quota.title")}</span>
				{stale && (
					<span
						className="fd-quota-stale"
						data-testid="quota-stale"
						title={t("quota.stale")}
					>
						{lastUpdatedAt
							? t("quota.lastUpdated", { time: formatTimeOfDay(lastUpdatedAt) })
							: t("quota.stale")}
					</span>
				)}
				<div className="fd-quota-strip-actions">
					{!collapsed && (
						<button
							type="button"
							data-testid="quota-refresh"
							className="fd-quota-modal-btn"
							onClick={() => void doRefresh()}
							disabled={refreshing}
							title={t("quota.refresh")}
							aria-label={t("quota.refresh")}
						>
							<ArrowClockwiseIcon
								size={14}
								className={refreshing ? "fd-spin" : undefined}
							/>
						</button>
					)}
					<button
						type="button"
						data-testid="quota-collapse"
						className="fd-quota-modal-btn"
						onClick={toggleCollapsed}
						aria-expanded={!collapsed}
						title={collapsed ? t("quota.expand") : t("quota.collapse")}
						aria-label={collapsed ? t("quota.expand") : t("quota.collapse")}
					>
						{collapsed ? (
							<CaretDownIcon size={14} />
						) : (
							<CaretUpIcon size={14} />
						)}
					</button>
				</div>
			</div>

			{!collapsed && (
				<div className="fd-quota-badges">
					{models.map((m) => (
						<QuotaBadge
							key={m.key}
							model={m}
							barMode={barMode}
							onClick={() => onBadgeClick(m)}
						/>
					))}
				</div>
			)}

			{/* The modal is where a partial provider payload actually bites: the
			    badges read defensively, the modals read deep. A boundary of its own
			    keeps that throw off the strip, so the badges (and every other
			    provider's modal) survive it. `key` is one recovery path: React
			    discards a boundary whose key changed, so opening a different
			    provider gets a clean one, and closing and reopening this one
			    unmounts it outright. No fallback, matching the strip's own empty
			    state; the operator sees the click do nothing rather than a broken
			    dialog, and the badge itself still shows the numbers.
			    `resetKeys` covers the other recovery path: the SAME provider getting
			    a corrected payload on a later poll, with the modal still open (so
			    `key` is unchanged and the boundary would otherwise stay latched
			    until the operator opened another provider or collapsed the strip).
			    `open.snapshot.fetched_at` changes whenever the primary re-fetches,
			    so a fresh snapshot always gets a fresh chance to render. Re-clicking
			    the same badge does NOT change fetched_at, so it is correctly a
			    no-op here: the recovery signal is new data, not a repeated click. */}
			{open && (
				<ErrorBoundary key={open.key} resetKeys={[open.snapshot.fetched_at]}>
					<QuotaModalFor
						model={open}
						barMode={barMode}
						onToggleBarMode={toggleBarMode}
						onRefresh={() => void doRefresh()}
						isRefreshing={refreshing}
						onClose={() => setOpenKey(null)}
					/>
				</ErrorBoundary>
			)}
		</div>
	);
}

interface QuotaModalForProps {
	model: QuotaBadgeModel;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

/** Picks the modal for a badge and narrows its payload to that provider's shape. */
function QuotaModalFor({ model, ...rest }: QuotaModalForProps) {
	const common = {
		providerName: model.providerName,
		fetchedAt: model.snapshot.fetched_at,
		...rest,
	};

	switch (model.type) {
		case "nanogpt": {
			const p = payloadOf<NanoGPTUsage>(model.snapshot);
			return p ? <NanoGPTQuotaModal {...common} payload={p} /> : null;
		}
		case "zai-coding": {
			const p = payloadOf<ZAICodingQuotaResponse>(model.snapshot);
			return p ? <ZAICodingQuotaModal {...common} payload={p} /> : null;
		}
		case "kimi-code": {
			const p = payloadOf<KimiCodeQuotaResponse>(model.snapshot);
			return p ? <KimiCodeQuotaModal {...common} payload={p} /> : null;
		}
		case "minimax": {
			const p = payloadOf<MiniMaxQuotaResponse>(model.snapshot);
			return p ? <MiniMaxQuotaModal {...common} payload={p} /> : null;
		}
		case "openrouter": {
			const p = payloadOf<OpenRouterBalance>(model.snapshot);
			return p ? <OpenRouterQuotaModal {...common} payload={p} /> : null;
		}
		case "neuralwatt": {
			const p = payloadOf<NeuralWattQuotaResponse>(model.snapshot);
			return p ? <NeuralWattQuotaModal {...common} payload={p} /> : null;
		}
		// DeepSeek and Ollama Cloud never open a modal; onBadgeClick refreshes.
		case "deepseek":
		case "ollama-cloud":
			return null;
	}
}
