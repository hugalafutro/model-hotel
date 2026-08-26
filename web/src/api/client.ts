// The dashboard's typed API surface. The fetch plumbing lives in ./http and
// each dashboard area's endpoints in ./endpoints/*; this module re-exports the
// helpers pages rely on and assembles the one `api` object they call.
import {
	auth,
	demoLogin,
	github,
	oidc,
	publicConfig,
	totp,
	userTotp,
	webauthn,
} from "./endpoints/auth";
import { chat } from "./endpoints/chat";
import { appLogs, audit, logs, stats } from "./endpoints/logs";
import { failoverGroups, models } from "./endpoints/models";
import { discovery, providers } from "./endpoints/providers";
import {
	alert,
	backups,
	settings,
	system,
	version,
} from "./endpoints/settings";
import { users, virtualKeys } from "./endpoints/users";

export {
	API_BASE,
	ApiError,
	buildQueryString,
	buildUrl,
	clearAuth,
	fetchJSONWithServerNow,
	getAuthHeaders,
	getCsrfToken,
	isAuthenticated,
	serverNowFromResponse,
} from "./http";

export const api = {
	publicConfig,
	demoLogin,
	providers,
	discovery,
	models,
	logs,
	appLogs,
	stats,
	settings,
	alert,
	version,
	virtualKeys,
	system,
	chat,
	failoverGroups,
	backups,
	webauthn,
	totp,
	oidc,
	github,
	auth,
	userTotp,
	audit,
	users,
};
