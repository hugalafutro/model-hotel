import type { Provider } from "../../../api/types";
import { findProviderAtAddress } from "../duplicateAddress";

function providerAt(
	id: string,
	name: string,
	base_url: string,
	provider_type = "koboldcpp",
): Provider {
	return {
		id,
		name,
		base_url,
		provider_type,
		masked_key: "N/A",
		enabled: true,
		autodiscovery_enabled: true,
		scheduled_disable_on: null,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2026-08-17T00:00:00Z",
		updated_at: "2026-08-17T00:00:00Z",
		model_count: 1,
		total_tokens: 0,
	};
}

const providers = [
	providerAt("p1", "KoboldCpp 141", "http://192.168.1.141:5005/v1"),
	providerAt("p2", "LMStudio 141", "http://192.168.1.141:11234/v1", "lmstudio"),
];

describe("findProviderAtAddress", () => {
	it("matches the same server however either side spells the address", () => {
		for (const typed of [
			"http://192.168.1.141:5005",
			"http://192.168.1.141:5005/",
			"http://192.168.1.141:5005/v1",
			"http://192.168.1.141:5005/v1/",
			"http://192.168.1.141:5005/v1  ",
		]) {
			expect(findProviderAtAddress(providers, typed)?.name).toBe(
				"KoboldCpp 141",
			);
		}
	});

	it("does not match a different server", () => {
		// Same host, different port: the other server on that box.
		expect(
			findProviderAtAddress(providers, "http://192.168.1.141:11234")?.name,
		).toBe("LMStudio 141");
		// Nothing configured here at all.
		expect(
			findProviderAtAddress(providers, "http://192.168.1.163:5005"),
		).toBeNull();
		expect(
			findProviderAtAddress(providers, "http://192.168.1.141:5006"),
		).toBeNull();
	});

	it("ignores the provider being edited, so its own address is not a clash", () => {
		expect(
			findProviderAtAddress(providers, "http://192.168.1.141:5005", "p1"),
		).toBeNull();
		// A different row at that address is still reported.
		expect(
			findProviderAtAddress(providers, "http://192.168.1.141:5005", "p2")?.name,
		).toBe("KoboldCpp 141");
	});

	it("returns nothing when there is no address or no provider list", () => {
		expect(findProviderAtAddress(providers, "")).toBeNull();
		expect(findProviderAtAddress(providers, "   ")).toBeNull();
		expect(findProviderAtAddress(undefined, "http://x:1/v1")).toBeNull();
		expect(findProviderAtAddress([], "http://x:1/v1")).toBeNull();
	});

	it("still compares an address too malformed to parse", () => {
		const malformed = [providerAt("p3", "Broken", "not a url/v1")];
		expect(findProviderAtAddress(malformed, "not a url")?.name).toBe("Broken");
		expect(findProviderAtAddress(malformed, "other")).toBeNull();
	});
});
