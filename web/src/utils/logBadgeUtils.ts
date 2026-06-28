/* =========================================================
   Shared utility functions for log badge rendering
   ===================================================== */

export const getLevelBadgeVariant = (level: string) => {
	switch (level) {
		case "error":
			return "error" as const;
		case "warning":
			return "warning" as const;
		case "debug":
			// Lowest severity — neutral/gray, distinct from info's color.
			return "muted" as const;
		default:
			return "info" as const;
	}
};

export const getSourceBadgeClasses = (source: string) => {
	switch (source) {
		case "auth":
			return "bg-purple-900/30 text-purple-400";
		case "proxy":
			return "bg-cyan-900/30 text-cyan-400";
		case "resolve":
			return "bg-teal-900/30 text-teal-400";
		case "discovery":
			return "bg-emerald-900/30 text-emerald-400";
		case "failover":
			return "bg-slate-700/50 text-slate-300";
		case "ratelimit":
			return "bg-amber-900/30 text-amber-400";
		case "vkey":
		case "admin":
			return "bg-pink-900/30 text-pink-400";
		case "settings":
			return "bg-indigo-900/30 text-indigo-400";
		case "events":
			return "bg-violet-900/30 text-violet-400";
		case "docker":
			return "bg-sky-900/30 text-sky-400";
		case "keycache":
		case "model":
		case "provider":
		case "cache":
		case "db":
			return "bg-lime-900/30 text-lime-400";
		case "access":
			return "bg-fuchsia-900/30 text-fuchsia-400";
		case "server":
		case "startup":
		case "retention":
			return "bg-blue-900/30 text-blue-400";
		case "circuit-breaker":
			return "bg-orange-900/30 text-orange-400";
		case "configsync":
		case "fleet":
			// HA / fleet membership — matches the indigo HA chip in the error shelf.
			return "bg-indigo-900/30 text-indigo-400";
		case "modelsdev":
			return "bg-rose-900/30 text-rose-400";
		case "applogs":
			return "bg-gray-700/30 text-gray-400";
		default:
			return "bg-gray-800/30 text-gray-400";
	}
};

export const formatTimestamp = (ts: string) => {
	try {
		const d = new Date(ts);
		if (Number.isNaN(d.getTime())) {
			return ts;
		}
		return d.toLocaleString("en-US", {
			year: "numeric",
			month: "2-digit",
			day: "2-digit",
			hour: "2-digit",
			minute: "2-digit",
			second: "2-digit",
			hour12: false,
		});
	} catch {
		return ts;
	}
};
