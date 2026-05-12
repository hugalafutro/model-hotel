import { http, HttpResponse } from "msw";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { ProviderQuotaPanel } from "../ProviderQuotaPanel";

describe("ProviderQuotaPanel", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("renders null when no providers", () => {
		server.use(
			http.get("/api/providers", () => {
				return HttpResponse.json([]);
			}),
		);
		const { container } = renderWithProviders(<ProviderQuotaPanel />);
		expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
	});

	it("renders null when quota is disabled", () => {
		const getItemSpy = vi.spyOn(Storage.prototype, "getItem");
		getItemSpy.mockImplementation((key) => {
			if (key === "sidebarQuotaRefreshMin") return "0";
			return null;
		});
		const { container } = renderWithProviders(<ProviderQuotaPanel />);
		expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
		getItemSpy.mockRestore();
	});

	it("handles refresh interval of 0 (disabled)", () => {
		const getItemSpy = vi.spyOn(Storage.prototype, "getItem");
		getItemSpy.mockImplementation((key) => {
			if (key === "sidebarQuotaRefreshMin") return "0";
			return null;
		});
		const { container } = renderWithProviders(<ProviderQuotaPanel />);
		expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
		getItemSpy.mockRestore();
	});
});