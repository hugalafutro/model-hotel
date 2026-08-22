import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../api/client";
import type { Model, Provider } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { formatDate } from "../../utils/format";
import { VirtualModelTable } from "../VirtualModelTable";

// Mock @tanstack/react-virtual (same pattern as VirtualLogTable/AppLogTable)
const mockGetVirtualItems = vi.fn();
const mockGetTotalSize = vi.fn();
const mockMeasureElement = vi.fn();

vi.mock("@tanstack/react-virtual", () => ({
	useVirtualizer: vi.fn(() => ({
		getVirtualItems: mockGetVirtualItems,
		getTotalSize: mockGetTotalSize,
		measureElement: mockMeasureElement,
	})),
}));

// Mock useBidirectionalFetch
const mockUseBidirectionalFetch = vi.fn();
vi.mock("../../hooks/useBidirectionalFetch", () => ({
	useBidirectionalFetch: (...args: unknown[]) =>
		mockUseBidirectionalFetch(...args),
}));

// Mock api client
vi.mock("../../api/client", () => ({
	api: {
		models: {
			cursor: vi.fn(),
		},
	},
	// EventProvider (mounted by renderWithProviders) reads the cookie-derived
	// auth signal; the fetch helpers ride along for any component network calls.
	isAuthenticated: vi.fn(() => true),
	clearAuth: vi.fn(),
	getAuthHeaders: vi.fn(() => ({ "Content-Type": "application/json" })),
	API_BASE: "",
}));

// Helper to create mock Model
function createModel(overrides: Partial<Model> = {}): Model {
	return {
		id: "model-001",
		model_id: "test-model-v1",
		name: "Test Model",
		description: "A test model",
		display_name: "Test Model v1",
		provider_id: "provider-001",
		provider_name: "Test Provider",
		provider_enabled: true,
		capabilities: '{"streaming":true,"vision":false}',
		params: "{}",
		modality: "text",
		input_modalities: "text",
		output_modalities: "text",
		context_length: 8192,
		max_output_tokens: 4096,
		input_price_per_million: 0.5,
		input_price_per_million_cache_hit: 0.1,
		output_price_per_million: 1.5,
		owned_by: "test-provider",
		enabled: true,
		disabled_manually: false,
		price_customized: false,
		created_at: "2026-01-15T10:00:00Z",
		last_seen_at: "2026-05-11T08:30:00Z",
		...overrides,
	};
}

const defaultHookReturn = {
	entries: [] as Model[],
	total: 0,
	hasBefore: false,
	hasAfter: false,
	isLoadingInitial: false,
	isLoadingBefore: false,
	isLoadingAfter: false,
	error: null as string | null,
	fetchInitial: vi.fn(),
	fetchNewer: vi.fn(),
	fetchOlder: vi.fn(),
	reset: vi.fn(),
};

function setupTable(overrides: Partial<typeof defaultHookReturn> = {}) {
	mockUseBidirectionalFetch.mockReturnValue({
		...defaultHookReturn,
		...overrides,
	});
}

function setupWithEntries(
	entries: Model[],
	extra: Partial<typeof defaultHookReturn> = {},
) {
	mockGetVirtualItems.mockReturnValue(
		entries.map((e, i) => ({
			index: i,
			key: e.id,
			start: i * 45,
			end: (i + 1) * 45,
		})),
	);
	mockGetTotalSize.mockReturnValue(entries.length * 45);
	setupTable({ entries, total: entries.length, ...extra });
}

describe("VirtualModelTable", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		mockGetVirtualItems.mockReturnValue([]);
		mockGetTotalSize.mockReturnValue(0);
		mockMeasureElement.mockImplementation(() => {});
		setupTable();
	});

	describe("Empty state", () => {
		it("renders 'No models found' when entries is empty", () => {
			setupTable({ entries: [], total: 0 });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("No models found")).toBeInTheDocument();
		});

		it("renders '0 / 0' in footer when entries empty", () => {
			setupTable({ entries: [], total: 0 });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("0 / 0")).toBeInTheDocument();
		});

		it("renders loading indicator when isLoadingInitial", () => {
			setupTable({ entries: [], total: 0, isLoadingInitial: true });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Loading…")).toBeInTheDocument();
		});

		it("renders loading newer indicator when isLoadingBefore", () => {
			const entries = [createModel()];
			setupWithEntries(entries, { isLoadingBefore: true });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Loading newer…")).toBeInTheDocument();
		});

		it("renders loading older indicator when isLoadingAfter", () => {
			const entries = [createModel()];
			setupWithEntries(entries, { isLoadingAfter: true });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Loading older…")).toBeInTheDocument();
		});
	});

	describe("Sorting", () => {
		it("toggles sort direction to desc when clicking Name header (same field, was asc)", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Default is name asc, so clicking name toggles to desc
			const nameHeader = screen.getByLabelText("Sort by model name");
			fireEvent.click(nameHeader);
			expect(screen.getByText("↓")).toBeInTheDocument();
		});

		it("changes sort field when clicking Discovered header (different field)", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const discoveredHeader = screen.getByLabelText("Sort by discovered date");
			fireEvent.click(discoveredHeader);
			// New field defaults to asc
			expect(screen.getByText("↑")).toBeInTheDocument();
		});

		it("sorts by context when clicking Ctx header", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const ctxHeader = screen.getByLabelText("Sort by context length");
			fireEvent.click(ctxHeader);
			expect(screen.getByText("↑")).toBeInTheDocument();
		});

		it("sorts by output when clicking Max Out header", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const outputHeader = screen.getByLabelText("Sort by max output tokens");
			fireEvent.click(outputHeader);
			expect(screen.getByText("↑")).toBeInTheDocument();
		});

		it("sorts by status when clicking Status header", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const statusHeader = screen.getByLabelText("Sort by status");
			fireEvent.click(statusHeader);
			expect(screen.getByText("↑")).toBeInTheDocument();
		});
	});

	describe("getCursor", () => {
		it("generates name cursor by default (sort.field=name)", () => {
			const mockGetCursor = vi.fn();
			const entries = [
				createModel({ id: "m1", name: "GPT-4", model_id: "gpt-4" }),
			];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable />);

			const cursor = mockGetCursor(entries[0]);
			expect(cursor).toBeDefined();
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("name");
			expect(decoded.name).toBe("GPT-4");
			expect(decoded.model_id).toBe("gpt-4");
		});

		it("generates discovered cursor when sort.field=discovered", () => {
			const mockGetCursor = vi.fn();
			const entries = [
				createModel({ id: "m2", last_seen_at: "2026-05-23T10:00:00Z" }),
			];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable />);

			// Click to change sort to discovered
			fireEvent.click(screen.getByLabelText("Sort by discovered date"));

			const cursor = mockGetCursor(entries[0]);
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("discovered");
			expect(decoded.last_seen_at).toBe("2026-05-23T10:00:00Z");
		});

		it("generates context cursor when sort.field=context", () => {
			const mockGetCursor = vi.fn();
			const entries = [createModel({ id: "m3", context_length: 16384 })];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable />);

			fireEvent.click(screen.getByLabelText("Sort by context length"));

			const cursor = mockGetCursor(entries[0]);
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("context");
			expect(decoded.context_length).toBe(16384);
		});

		it("generates output cursor when sort.field=output", () => {
			const mockGetCursor = vi.fn();
			const entries = [createModel({ id: "m4", max_output_tokens: 8192 })];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable />);

			fireEvent.click(screen.getByLabelText("Sort by max output tokens"));

			const cursor = mockGetCursor(entries[0]);
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("output");
			expect(decoded.max_output_tokens).toBe(8192);
		});

		it("generates provider cursor when sort.field=provider", () => {
			const mockGetCursor = vi.fn();
			const entries = [createModel({ id: "m5", provider_name: "OpenAI" })];
			const providers = [{ id: "p1", name: "OpenAI" }] as unknown as Provider[];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable providers={providers} />);

			fireEvent.click(screen.getByLabelText("Sort by provider name"));

			const cursor = mockGetCursor(entries[0]);
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("provider");
			expect(decoded.provider_name).toBe("OpenAI");
		});

		it("generates status cursor when sort.field=status", () => {
			const mockGetCursor = vi.fn();
			const entries = [
				createModel({ id: "m6", enabled: true, disabled_manually: false }),
			];

			mockUseBidirectionalFetch.mockImplementation(
				({ getCursor }: { getCursor?: (m: Model) => string }) => {
					if (getCursor) mockGetCursor.mockImplementation(getCursor);
					return { ...defaultHookReturn, entries, total: 1 };
				},
			);
			renderWithProviders(<VirtualModelTable />);

			fireEvent.click(screen.getByLabelText("Sort by status"));

			const cursor = mockGetCursor(entries[0]);
			const decoded = JSON.parse(atob(cursor));
			expect(decoded.sort_by).toBe("status");
			expect(decoded.status_sort).toBe(0); // enabled && !disabled_manually
		});
	});

	describe("Filtering", () => {
		it("updates search query when typing in search input", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const searchInput = screen.getByPlaceholderText("Search models…");
			fireEvent.change(searchInput, { target: { value: "gpt" } });
			expect(searchInput).toHaveValue("gpt");
		});

		it("renders output pills for generation models", () => {
			const entries = [
				createModel({
					id: "model-gen",
					model_id: "z-image-turbo",
					name: "Z Image Turbo",
					capabilities: "{}",
					modality: "image",
					output_modalities: '["image"]',
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// The row pill is a span; the filter row renders a button with the
			// same label.
			const pills = screen.getAllByText("Image out");
			expect(pills.some((el) => el.tagName === "SPAN")).toBe(true);
		});

		it("renders capability filter buttons from existing model caps", () => {
			const entries = [
				createModel({
					capabilities: '{"vision":true,"tool_calling":true,"reasoning":true}',
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Capabilities from existing models should show as filter buttons
			expect(screen.getAllByText("Vision").length).toBeGreaterThanOrEqual(1);
			expect(screen.getAllByText("Tools").length).toBeGreaterThanOrEqual(1);
			expect(screen.getAllByText("Reasoning").length).toBeGreaterThanOrEqual(1);
		});

		it("shows clear button when capability filters are active", () => {
			const entries = [
				createModel({ capabilities: '{"vision":true,"tool_calling":true}' }),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Click a capability filter to activate it (use first Vision — the filter button)
			const visionButtons = screen.getAllByText("Vision");
			fireEvent.click(visionButtons[0]);

			// Clear button (✕) should appear
			expect(screen.getByText("✕")).toBeInTheDocument();
		});

		it("offers capability filter pills even when no loaded row has that cap", () => {
			// Same reasoning as output pills: capability filtering is
			// server-side, so a PDF-capable model may exist outside the
			// loaded window.
			const entries = [createModel({ id: "model-plain", capabilities: "{}" })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const pdfPill = screen.getByText("PDF");
			expect(pdfPill.tagName).toBe("BUTTON");
			fireEvent.click(pdfPill);

			const lastCall =
				mockUseBidirectionalFetch.mock.calls[
					mockUseBidirectionalFetch.mock.calls.length - 1
				][0];
			expect(lastCall.filters.capabilities).toBe("pdf_upload");
		});

		it("offers output filter pills even when no loaded row has that output", () => {
			// Server-side pagination: an image generator may exist outside the
			// loaded window, so the pill must render regardless of entries.
			const entries = [
				createModel({
					id: "model-chat",
					model_id: "gpt-4o",
					output_modalities: '["text"]',
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			const imageOut = screen.getByText("Image out");
			expect(imageOut.tagName).toBe("BUTTON");
			fireEvent.click(imageOut);

			const lastCall =
				mockUseBidirectionalFetch.mock.calls[
					mockUseBidirectionalFetch.mock.calls.length - 1
				][0];
			expect(lastCall.filters.outputs).toBe("image");
		});

		it("offers an output filter pill and sends the outputs filter", () => {
			const entries = [
				createModel({
					id: "model-gen",
					model_id: "z-image-turbo",
					capabilities: "{}",
					modality: "image",
					output_modalities: '["image"]',
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// One "Image out" in the filter row, one pill in the model row.
			const imageOutButtons = screen.getAllByText("Image out");
			expect(imageOutButtons.length).toBeGreaterThanOrEqual(2);
			fireEvent.click(imageOutButtons[0]);

			const lastCall =
				mockUseBidirectionalFetch.mock.calls[
					mockUseBidirectionalFetch.mock.calls.length - 1
				][0];
			expect(lastCall.filters.outputs).toBe("image");

			// Clear button resets the output filter too.
			fireEvent.click(screen.getByText("✕"));
			const afterClear =
				mockUseBidirectionalFetch.mock.calls[
					mockUseBidirectionalFetch.mock.calls.length - 1
				][0];
			expect(afterClear.filters.outputs).toBeUndefined();
		});
	});

	describe("Rendering", () => {
		it("renders model name in row", () => {
			const entries = [createModel({ name: "GPT-4o" })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("GPT-4o")).toBeInTheDocument();
		});

		it("renders provider name in row when providers prop given", () => {
			const entries = [createModel({ provider_name: "OpenAI" })];
			const providers = [{ id: "p1", name: "OpenAI" }] as unknown as Provider[];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable providers={providers} />);
			expect(screen.getByText("OpenAI")).toBeInTheDocument();
		});

		it("renders 'Enabled' status badge for active models", () => {
			const entries = [
				createModel({ enabled: true, disabled_manually: false }),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Enabled")).toBeInTheDocument();
		});

		it("renders 'Manually Disabled' status badge for manually disabled models", () => {
			const entries = [createModel({ enabled: true, disabled_manually: true })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Manually Disabled")).toBeInTheDocument();
		});

		it("renders 'Disabled' status badge for disabled models", () => {
			const entries = [
				createModel({ enabled: false, disabled_manually: false }),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("Disabled")).toBeInTheDocument();
		});

		it("shows discovery tooltip on badge for discovery-disabled models", () => {
			const entries = [
				createModel({
					enabled: false,
					disabled_manually: false,
					price_customized: false,
					last_seen_at: "2026-05-11T08:30:00Z",
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			const badge = screen.getByTestId("disabled-by-discovery");
			expect(badge).toHaveAttribute(
				"title",
				expect.stringContaining(formatDate("2026-05-11T08:30:00Z")),
			);
		});

		it("does not show discovery tooltip for manually disabled models", () => {
			const entries = [createModel({ enabled: true, disabled_manually: true })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(
				screen.queryByTestId("disabled-by-discovery"),
			).not.toBeInTheDocument();
		});

		it("renders capability badges on model rows", () => {
			const entries = [
				createModel({
					capabilities: '{"vision":true,"tool_calling":true,"reasoning":true}',
				}),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Cap badges in row (not filter buttons)
			const visionBadges = screen.getAllByText("Vision");
			expect(visionBadges.length).toBeGreaterThanOrEqual(1);
		});

		it("renders pagination info when entries exist", () => {
			const entries = [createModel()];
			setupWithEntries(entries, { total: 1 });
			renderWithProviders(<VirtualModelTable />);
			expect(screen.getByText("1–1 / 1")).toBeInTheDocument();
		});

		it("calls onModelClick when model row is clicked", () => {
			const onModelClick = vi.fn();
			const entries = [createModel({ name: "ClickMe" })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable onModelClick={onModelClick} />);

			fireEvent.click(screen.getByText("ClickMe"));
			expect(onModelClick).toHaveBeenCalledWith(entries[0]);
		});

		it("does not add cursor-pointer class when onModelClick is not provided", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			const { container } = renderWithProviders(<VirtualModelTable />);

			const rows = container.querySelectorAll("tbody tr");
			expect(rows.length).toBeGreaterThan(0);
			expect(rows[0].className).not.toContain("cursor-pointer");
		});

		it("renders model name fallback to proxyModelID when name is empty", () => {
			const entries = [createModel({ name: "", model_id: "gpt-4" })];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			// proxyModelID(provider, model_id) format — appears twice (name fallback + monospace sub-line)
			const matches = screen.getAllByText("Test-Provider/gpt-4");
			expect(matches.length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Provider column", () => {
		it("shows provider column when providers prop is given", () => {
			const entries = [createModel()];
			const providers = [{ id: "p1", name: "OpenAI" }] as unknown as Provider[];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable providers={providers} />);
			expect(
				screen.getByLabelText("Sort by provider name"),
			).toBeInTheDocument();
		});

		it("hides provider column when no providers prop", () => {
			const entries = [createModel()];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);
			expect(
				screen.queryByLabelText("Sort by provider name"),
			).not.toBeInTheDocument();
		});
	});

	describe("Scroll edge threshold", () => {
		it("calls fetchNewer when scrollTop < 500 and hasBefore=true", () => {
			const fetchNewer = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, { hasBefore: true, fetchNewer });

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 100,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchNewer).toHaveBeenCalled();
		});

		it("does NOT call fetchNewer when hasBefore=false", () => {
			const fetchNewer = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, { hasBefore: false, fetchNewer });

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 100,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchNewer).not.toHaveBeenCalled();
		});

		it("does NOT call fetchNewer when isLoadingBefore=true", () => {
			const fetchNewer = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, {
				hasBefore: true,
				isLoadingBefore: true,
				fetchNewer,
			});

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 100,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchNewer).not.toHaveBeenCalled();
		});

		it("calls fetchOlder when near bottom and hasAfter=true", () => {
			const fetchOlder = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, { hasAfter: true, fetchOlder });

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			// scrollTop + clientHeight near scrollHeight → near bottom
			Object.defineProperty(scrollEl, "scrollTop", {
				value: 600,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchOlder).toHaveBeenCalled();
		});

		it("does NOT call fetchOlder when hasAfter=false", () => {
			const fetchOlder = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, { hasAfter: false, fetchOlder });

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 600,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchOlder).not.toHaveBeenCalled();
		});

		it("does NOT call fetchOlder when isLoadingAfter=true", () => {
			const fetchOlder = vi.fn();
			const entries = [createModel()];
			setupWithEntries(entries, {
				hasAfter: true,
				isLoadingAfter: true,
				fetchOlder,
			});

			const { container } = renderWithProviders(<VirtualModelTable />);
			const scrollEl = container.querySelector(
				'[class*="ui-card overflow-y-auto"]',
			);
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 600,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 1000,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				configurable: true,
			});

			fireEvent.scroll(scrollEl);
			expect(fetchOlder).not.toHaveBeenCalled();
		});
	});

	describe("refreshTrigger", () => {
		it("calls reset and fetchInitial when refreshTrigger changes", () => {
			const reset = vi.fn();
			const fetchInitial = vi.fn();
			setupTable({ reset, fetchInitial });

			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={0} />,
			);
			reset.mockClear();
			fetchInitial.mockClear();

			rerender(<VirtualModelTable refreshTrigger={1} />);
			expect(reset).toHaveBeenCalled();
			expect(fetchInitial).toHaveBeenCalled();
		});

		it("does not call reset when refreshTrigger is unchanged", () => {
			const reset = vi.fn();
			const fetchInitial = vi.fn();
			setupTable({ reset, fetchInitial });

			const { rerender } = renderWithProviders(
				<VirtualModelTable refreshTrigger={1} />,
			);
			reset.mockClear();
			fetchInitial.mockClear();

			rerender(<VirtualModelTable refreshTrigger={1} />);
			expect(reset).not.toHaveBeenCalled();
		});

		it("does not call reset when refreshTrigger is undefined", () => {
			const reset = vi.fn();
			const fetchInitial = vi.fn();
			setupTable({ reset, fetchInitial });

			const { rerender } = renderWithProviders(<VirtualModelTable />);
			reset.mockClear();
			fetchInitial.mockClear();

			// Rerender with refreshTrigger still undefined (prop not passed)
			rerender(<VirtualModelTable />);
			expect(reset).not.toHaveBeenCalled();
			expect(fetchInitial).not.toHaveBeenCalled();
		});
	});

	describe("Provider filter integration", () => {
		it("passes provider_id in filters when a provider filter is set", () => {
			const capturedFilters: Record<string, unknown>[] = [];
			const entries = [createModel({ capabilities: '{"vision":true}' })];
			const providers = [
				{ id: "p1", name: "OpenAI" },
				{ id: "p2", name: "Anthropic" },
			] as unknown as Provider[];

			mockUseBidirectionalFetch.mockImplementation(
				({ filters }: { filters: Record<string, unknown> }) => {
					capturedFilters.push(filters);
					return {
						...defaultHookReturn,
						entries,
						total: entries.length,
					};
				},
			);
			mockGetVirtualItems.mockReturnValue(
				entries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 45,
					end: (i + 1) * 45,
				})),
			);
			mockGetTotalSize.mockReturnValue(entries.length * 45);

			renderWithProviders(
				<VirtualModelTable providers={providers} providerFilter="p1" />,
			);

			// Verify filters passed to useBidirectionalFetch include provider_id
			const lastFilter = capturedFilters[capturedFilters.length - 1];
			expect(lastFilter.provider_id).toBe("p1");
			expect(lastFilter.provider_enabled).toBeUndefined();
		});

		it("passes provider_enabled in filters as a string when scoped", () => {
			const capturedFilters: Record<string, unknown>[] = [];
			mockUseBidirectionalFetch.mockImplementation(
				({ filters }: { filters: Record<string, unknown> }) => {
					capturedFilters.push(filters);
					return { ...defaultHookReturn, entries: [], total: 0 };
				},
			);
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(<VirtualModelTable providerEnabled={false} />);

			const lastFilter = capturedFilters[capturedFilters.length - 1];
			expect(lastFilter.provider_enabled).toBe("false");
		});

		it("maps the provider_enabled filter back to a boolean for the API", async () => {
			type FetchFn = (params: Record<string, unknown>) => Promise<unknown>;
			let fetchFn: FetchFn | undefined;
			mockUseBidirectionalFetch.mockImplementation(
				({ fetchFn: fn }: { fetchFn: FetchFn }) => {
					fetchFn = fn;
					return { ...defaultHookReturn, entries: [], total: 0 };
				},
			);
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);
			const cursor = vi.mocked(api.models.cursor);
			cursor.mockResolvedValue({
				entries: [],
				enabled_total: 0,
				parked_total: 0,
				disabled_total: 0,
				total: 0,
				has_before: false,
				has_after: false,
			});

			const onTotalChange = vi.fn();
			renderWithProviders(
				<VirtualModelTable
					providerEnabled={true}
					onTotalChange={onTotalChange}
				/>,
			);
			if (!fetchFn) throw new Error("fetchFn not captured");

			await fetchFn({
				direction: "after",
				limit: 10,
				sort_dir: "asc",
				provider_enabled: "true",
			});
			expect(cursor).toHaveBeenLastCalledWith(
				expect.objectContaining({ provider_enabled: true }),
			);

			await fetchFn({ direction: "after", limit: 10, sort_dir: "asc" });
			expect(cursor).toHaveBeenLastCalledWith(
				expect.objectContaining({ provider_enabled: undefined }),
			);
		});

		it("reports the counts from the hook's accepted response, not from the fetch", () => {
			// The counts ride the hook's generation guard: the component reads
			// lastResponse, so a stale in-flight reply can never leak them.
			mockUseBidirectionalFetch.mockImplementation(() => ({
				...defaultHookReturn,
				entries: [],
				total: 7,
				lastResponse: {
					entries: [],
					total: 7,
					enabled_total: 5,
					parked_total: 1,
					disabled_total: 0,
					has_before: false,
					has_after: false,
				},
			}));
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);
			const onTotalChange = vi.fn();

			renderWithProviders(<VirtualModelTable onTotalChange={onTotalChange} />);

			expect(onTotalChange).toHaveBeenLastCalledWith({
				total: 7,
				enabled: 5,
				parked: 1,
			});
		});

		it("reports zero counts after a reset, when the hook has no accepted response", () => {
			mockUseBidirectionalFetch.mockImplementation(() => ({
				...defaultHookReturn,
				entries: [],
				total: 0,
				lastResponse: null,
			}));
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);
			const onTotalChange = vi.fn();

			renderWithProviders(<VirtualModelTable onTotalChange={onTotalChange} />);

			expect(onTotalChange).toHaveBeenLastCalledWith({
				total: 0,
				enabled: 0,
				parked: 0,
			});
		});

		it("shows a parked pill instead of the enabled state for a disabled provider's model", () => {
			const entries = [
				createModel({ id: "m-parked", enabled: true, provider_enabled: false }),
			];
			setupWithEntries(entries);

			renderWithProviders(<VirtualModelTable />);

			expect(screen.getByText("Parked")).toBeInTheDocument();
			expect(screen.queryByText("Enabled")).not.toBeInTheDocument();
		});
	});

	describe("Capability filter toggle off", () => {
		it("deactivates a capability filter when clicking it twice", () => {
			const entries = [
				createModel({ capabilities: '{"vision":true,"tool_calling":true}' }),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Click Vision to activate (first Vision button is the filter button)
			const visionButtons = screen.getAllByText("Vision");
			fireEvent.click(visionButtons[0]);

			// ✕ clear button should appear confirming filter is active
			expect(screen.getByText("✕")).toBeInTheDocument();

			// Click Vision again to deactivate
			const visionButtonsAfter = screen.getAllByText("Vision");
			fireEvent.click(visionButtonsAfter[0]);

			// ✕ should disappear since no filters are active
			expect(screen.queryByText("✕")).not.toBeInTheDocument();
		});
	});

	describe("Clear capability filter button", () => {
		it("clears all capability filters when clicking ✕ button", () => {
			const entries = [
				createModel({ capabilities: '{"vision":true,"tool_calling":true}' }),
			];
			setupWithEntries(entries);
			renderWithProviders(<VirtualModelTable />);

			// Activate Vision filter
			const visionButtons = screen.getAllByText("Vision");
			fireEvent.click(visionButtons[0]);

			// ✕ should be visible
			expect(screen.getByText("✕")).toBeInTheDocument();

			// Click ✕ to clear all
			fireEvent.click(screen.getByText("✕"));

			// ✕ should disappear
			expect(screen.queryByText("✕")).not.toBeInTheDocument();
		});
	});

	describe("Delete Disabled Button", () => {
		// The button and the delete are filter-wide: the scroller only holds a
		// window of rows, so both read the server's disabled_total and the
		// confirm fetches every disabled row of the current filters itself.
		function setupWithDisabledTotal(disabledTotal: number, entries: Model[]) {
			vi.mocked(api.models.cursor).mockReset();
			mockGetVirtualItems.mockReturnValue(
				entries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 45,
					end: (i + 1) * 45,
				})),
			);
			mockGetTotalSize.mockReturnValue(entries.length * 45);
			setupTable({
				entries,
				total: entries.length,
				lastResponse: {
					entries,
					total: entries.length,
					enabled_total: entries.length - disabledTotal,
					parked_total: 0,
					disabled_total: disabledTotal,
					has_before: false,
					has_after: false,
				},
			} as Partial<typeof defaultHookReturn>);
		}

		function page(entries: Model[], hasAfter: boolean) {
			return {
				entries,
				total: entries.length,
				enabled_total: 0,
				parked_total: 0,
				disabled_total: entries.length,
				has_before: false,
				has_after: hasAfter,
			};
		}

		it("shows the filter-wide disabled count even when no disabled row is loaded", () => {
			setupWithDisabledTotal(24, [createModel()]);

			renderWithProviders(<VirtualModelTable onDeleteDisabled={vi.fn()} />);

			expect(screen.getByText("Delete 24 disabled")).toBeInTheDocument();
		});

		it("does not render delete disabled button when the filter has no disabled rows", () => {
			setupWithDisabledTotal(0, [
				createModel(),
				createModel({ id: "model-002", name: "Model 2" }),
			]);

			renderWithProviders(<VirtualModelTable onDeleteDisabled={vi.fn()} />);

			expect(
				screen.queryByRole("button", { name: /delete.*disabled/i }),
			).not.toBeInTheDocument();
		});

		it("fetches every disabled row of the current filters and deletes exactly those", async () => {
			const onDeleteDisabled = vi.fn();
			setupWithDisabledTotal(3, [createModel()]);
			const offscreen1 = createModel({
				id: "model-disabled-1",
				model_id: "grok-3",
				name: "grok-3",
				enabled: false,
			});
			const offscreen2 = createModel({
				id: "model-disabled-2",
				model_id: "kimi-k2.5",
				name: "kimi-k2.5",
				enabled: false,
			});
			const offscreen3 = createModel({
				id: "model-disabled-3",
				model_id: "qwen3.5-397b",
				name: "qwen3.5-397b",
				enabled: false,
			});
			const cursor = vi.mocked(api.models.cursor);
			cursor
				.mockResolvedValueOnce(page([offscreen1, offscreen2], true))
				.mockResolvedValueOnce(page([offscreen3], false));

			renderWithProviders(
				<VirtualModelTable
					onDeleteDisabled={onDeleteDisabled}
					providerFilter="provider-001"
					providerEnabled={true}
				/>,
			);

			fireEvent.click(screen.getByText("Delete 3 disabled"));

			// The dialog lists what the fetch returned, not what is scrolled in.
			expect(
				await screen.findByText("Delete Disabled Models"),
			).toBeInTheDocument();
			expect(screen.getByText("Test-Provider/grok-3")).toBeInTheDocument();
			expect(
				screen.getByText("Test-Provider/qwen3.5-397b"),
			).toBeInTheDocument();

			// Both pages carried the table's filters plus the disabled-only flag;
			// the second continued from the first page's last row.
			expect(cursor).toHaveBeenCalledTimes(2);
			expect(cursor.mock.calls[0][0]).toEqual(
				expect.objectContaining({
					cursor: undefined,
					direction: "after",
					limit: 200,
					sort_by: "name",
					provider_id: "provider-001",
					provider_enabled: true,
					enabled: false,
				}),
			);
			expect(cursor.mock.calls[1][0]).toEqual(
				expect.objectContaining({
					cursor: expect.stringMatching(/./),
					enabled: false,
					provider_id: "provider-001",
				}),
			);

			fireEvent.click(screen.getByText("Delete"));

			await waitFor(() => {
				expect(onDeleteDisabled).toHaveBeenCalledWith([
					"model-disabled-1",
					"model-disabled-2",
					"model-disabled-3",
				]);
			});
			expect(
				screen.queryByText("Delete Disabled Models"),
			).not.toBeInTheDocument();
		});

		it("reports a failed fetch and deletes nothing", async () => {
			const onDeleteDisabled = vi.fn();
			setupWithDisabledTotal(2, [createModel()]);
			vi.mocked(api.models.cursor).mockRejectedValueOnce(
				new Error("Failed to fetch models (cursor)"),
			);

			renderWithProviders(
				<VirtualModelTable onDeleteDisabled={onDeleteDisabled} />,
			);

			fireEvent.click(screen.getByText("Delete 2 disabled"));

			expect(
				await screen.findByText(
					"Failed to load disabled models: Failed to fetch models (cursor)",
				),
			).toBeInTheDocument();
			expect(
				screen.queryByText("Delete Disabled Models"),
			).not.toBeInTheDocument();
			expect(onDeleteDisabled).not.toHaveBeenCalled();
			// The button is usable again for a retry.
			expect(screen.getByText("Delete 2 disabled")).toBeEnabled();
		});

		it("closes confirm dialog on cancel", async () => {
			const onDeleteDisabled = vi.fn();
			setupWithDisabledTotal(1, [createModel()]);
			vi.mocked(api.models.cursor).mockResolvedValueOnce(
				page([createModel({ id: "model-disabled-1", enabled: false })], false),
			);

			renderWithProviders(
				<VirtualModelTable onDeleteDisabled={onDeleteDisabled} />,
			);

			fireEvent.click(screen.getByText("Delete 1 disabled"));
			expect(
				await screen.findByText("Delete Disabled Models"),
			).toBeInTheDocument();

			fireEvent.click(screen.getByText("Cancel"));

			await waitFor(() => {
				expect(
					screen.queryByText("Delete Disabled Models"),
				).not.toBeInTheDocument();
			});
			expect(onDeleteDisabled).not.toHaveBeenCalled();
		});
	});
});
