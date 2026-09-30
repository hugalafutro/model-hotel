import { ApiError } from "../../api/client";
import { providerTypeTranslationKeys } from "./constants";

function stringField(
	details: Record<string, unknown> | undefined,
	key: string,
): string {
	const value = details?.[key];
	return typeof value === "string" ? value : "";
}

/** Display name for a provider type: the translated label when the type is one
 * we know, the raw value otherwise (a newer server could name a type this build
 * has no label for). */
function typeLabel(type: string, t: (key: string) => string): string {
	const key = providerTypeTranslationKeys[type];
	return key ? t(key) : type;
}

/**
 * Phrases a coded refusal of a provider save in the operator's language: a
 * failed type check (naming the server that actually answered), a refused or
 * duplicate address, or a name the proxy could never route to. Returns null for
 * any other error, so callers fall back to the raw message.
 */
export function providerTypeGateMessage(
	err: unknown,
	t: (key: string, opts?: Record<string, string>) => string,
): string | null {
	if (!(err instanceof ApiError) || !err.code) return null;
	const expected = typeLabel(stringField(err.details, "expected"), t);
	switch (err.code) {
		case "provider_type_mismatch": {
			const detected = typeLabel(stringField(err.details, "detected"), t);
			const version = stringField(err.details, "detected_version");
			return version
				? t("providers.add.typeMismatchVersion", {
						detected,
						version,
						expected,
					})
				: t("providers.add.typeMismatch", { detected, expected });
		}
		case "provider_type_unconfirmed":
			return t("providers.add.typeUnconfirmed", { expected });
		case "provider_unreachable":
			return t("providers.add.serverUnreachable");
		case "provider_duplicate_address":
			return t("providers.add.duplicateAddressBlocked", {
				name: stringField(err.details, "existing"),
			});
		case "provider_url_rejected":
			// The backend's reason names the rule that refused the address
			// (allowlist, loopback, private range), which is what tells the
			// operator what to change.
			return t("providers.add.urlRejected", {
				detail: stringField(err.details, "error"),
			});
		case "provider_name_slash":
			return t("providers.add.nameHasSlash");
		case "provider_name_reserved":
			return t("providers.add.nameReserved");
		default:
			return null;
	}
}
