import { Link } from "react-router";
import type { CircuitBreakerStatus } from "../../api/types";
import { ProviderQuotaPanel } from "../ProviderQuotaPanel";
import { DiscoveryNavBadge } from "./DiscoveryNavBadge";
import { FailoverNavBadge } from "./FailoverNavBadge";
import type { DiscoveryBadge } from "./useDiscrepancyModal";
import type { NavIcon, useNavigation } from "./useNavigation";

type Navigation = ReturnType<typeof useNavigation>;

/**
 * The sidebar's item list. Two-faced pages show their current sub-mode with
 * the other one dimmed beside it; Failover carries the circuit counts while
 * any circuit is not closed; Models carries the discovery badge while there
 * is something to show and the modal is not already open.
 */
export function SidebarNav({
	navigation,
	subModeMap,
	handleSubModeToggle,
	isActive,
	navSep,
	cbStatus,
	discoveryBadge,
	discoveryOpen,
	onOpenDiscovery,
}: Navigation & {
	navSep: string;
	cbStatus: CircuitBreakerStatus | undefined;
	discoveryBadge: DiscoveryBadge;
	discoveryOpen: boolean;
	onOpenDiscovery: () => void;
}) {
	const showDiscovery =
		(discoveryBadge.claimCount > 0 ||
			discoveryBadge.informationalUnseen > 0 ||
			discoveryBadge.hasPinned) &&
		!discoveryOpen;
	return (
		<nav className="flex-1 min-h-0 px-4 py-2 overflow-y-auto">
			<ul className="space-y-0.5">
				{navigation.map((item) => {
					const sm = subModeMap[item.href];
					const currentMode = sm?.mode ?? "";
					const Icon: NavIcon =
						typeof item.icon === "function"
							? (item.icon as (mode: string) => NavIcon)(currentMode)
							: (item.icon as NavIcon);
					const active = isActive(item.href);
					const hasSubModes = Boolean(item.subModes);
					const currentSubLabel =
						hasSubModes && sm
							? item.subModes?.find((s) => s.value === sm.mode)?.label
							: null;
					const otherSub =
						hasSubModes && sm
							? item.subModes?.find((s) => s.value !== sm.mode)
							: null;

					return (
						<li key={item.name}>
							<Link
								to={item.href}
								onClick={
									hasSubModes ? handleSubModeToggle(item.href, item) : undefined
								}
								className={`sidebar-link flex items-center px-4 py-2 transition-colors ${
									active ? "sidebar-link-active" : "sidebar-link-inactive"
								}`}
							>
								<span className="mr-3 text-(--nav-icon)">
									<Icon size={18} strokeWidth={active ? 2.5 : 2} />
								</span>
								{hasSubModes && currentSubLabel ? (
									<span className="flex items-baseline gap-1.5">
										<span className={active ? "font-semibold" : ""}>
											{currentSubLabel}
										</span>
										<span className="text-(--text-muted) text-[10px] opacity-60">
											{navSep}
										</span>
										<span className="text-[11px] text-(--text-tertiary)">
											{otherSub?.label}
										</span>
									</span>
								) : item.href === "/failover" &&
									cbStatus &&
									(cbStatus.half_open > 0 || cbStatus.open > 0) ? (
									<span className="flex items-center gap-1.5">
										<span>{item.name}</span>
										<FailoverNavBadge cbStatus={cbStatus} navSep={navSep} />
									</span>
								) : item.href === "/models" && showDiscovery ? (
									<span className="flex items-center gap-1.5">
										<span>{item.name}</span>
										<DiscoveryNavBadge
											badge={discoveryBadge}
											onOpen={onOpenDiscovery}
										/>
									</span>
								) : (
									item.name
								)}
							</Link>
						</li>
					);
				})}
			</ul>
			<ProviderQuotaPanel />
		</nav>
	);
}
