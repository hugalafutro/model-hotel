import { ApiError } from "../../api/client";
import { providerTypeTranslationKeys } from "./constants";

/** Error codes the backend returns when an address cannot be accepted: a
 * self-hosted server that does not answer as the type it was added under, or a
 * URL the SSRF guard refuses outright. */
export const providerTypeGateCodes = [
	"provider_type_mismatch",
	"provider_type_unconfirmed",
	"provider_unreachable",
	"provider_url_rejected",
	"provider_duplicate_address",
] as const;

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
 * Phrases a failed provider-type check in the operator's language, naming the
 * server that actually answered. Returns null for any other error, so callers
 * fall back to the raw message.
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
		default:
			return null;
	}
}
