import { ApiError } from "../../../api/client";
import { providerTypeGateMessage } from "../typeGateError";

// The real t() is not needed here: these assertions are about which key and
// which interpolation values a code maps to, not about the English wording.
const t = (key: string, opts?: Record<string, string>): string =>
	opts ? `${key}(${JSON.stringify(opts)})` : key;

describe("providerTypeGateMessage", () => {
	it("names the detected server and its version on a mismatch", () => {
		const err = new ApiError("boom", 400, "provider_type_mismatch", {
			expected: "lmstudio",
			detected: "koboldcpp",
			detected_version: "1.119",
		});
		const msg = providerTypeGateMessage(err, t);
		expect(msg).toContain("providers.add.typeMismatchVersion");
		expect(msg).toContain("1.119");
		// Types are shown by their display labels, not their internal keys.
		expect(msg).toContain("providers.type_koboldcpp");
		expect(msg).toContain("providers.type_lmstudio");
	});

	it("drops the version clause when the server reports none", () => {
		const err = new ApiError("boom", 400, "provider_type_mismatch", {
			expected: "lmstudio",
			detected: "ollama",
		});
		expect(providerTypeGateMessage(err, t)).toContain(
			"providers.add.typeMismatch(",
		);
	});

	it("distinguishes a server that answered from one that did not", () => {
		const unconfirmed = new ApiError("boom", 400, "provider_type_unconfirmed", {
			expected: "ollama",
		});
		expect(providerTypeGateMessage(unconfirmed, t)).toContain(
			"providers.add.typeUnconfirmed",
		);

		const unreachable = new ApiError("boom", 400, "provider_unreachable", {
			expected: "ollama",
		});
		expect(providerTypeGateMessage(unreachable, t)).toBe(
			"providers.add.serverUnreachable",
		);
	});

	it("passes a type this build has no label for through unchanged", () => {
		const err = new ApiError("boom", 400, "provider_type_mismatch", {
			expected: "lmstudio",
			detected: "some-future-server",
		});
		expect(providerTypeGateMessage(err, t)).toContain("some-future-server");
	});

	it("names the provider already holding a duplicated address", () => {
		const err = new ApiError("boom", 409, "provider_duplicate_address", {
			existing: "KoboldCpp 141",
		});
		const msg = providerTypeGateMessage(err, t);
		expect(msg).toContain("providers.add.duplicateAddressBlocked");
		expect(msg).toContain("KoboldCpp 141");
	});

	it("explains a URL the guard refused, keeping the backend's reason", () => {
		const err = new ApiError("boom", 400, "provider_url_rejected", {
			error:
				'provider host "localhost" is not in ALLOWED_PROVIDER_HOSTS allowlist',
		});
		const msg = providerTypeGateMessage(err, t);
		expect(msg).toContain("providers.add.urlRejected");
		// The rule that refused it is what tells the operator what to change.
		expect(msg).toContain("ALLOWED_PROVIDER_HOSTS");
	});

	it("leaves every other error to the caller", () => {
		expect(providerTypeGateMessage(new Error("network down"), t)).toBeNull();
		expect(
			providerTypeGateMessage(new ApiError("nope", 409, undefined), t),
		).toBeNull();
		expect(
			providerTypeGateMessage(new ApiError("nope", 400, "some_other_code"), t),
		).toBeNull();
	});
});
