import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import { useModelEditor } from "../useModelEditor";

const mockModel: Model = {
	id: "model-001",
	model_id: "test-model-v1",
	name: "Test Model",
	description: "A test model",
	display_name: "Test Model v1",
	provider_id: "provider-001",
	provider_name: "Test Provider",
	capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
	params: '{"temperature":0.7,"max_tokens":4096}',
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
	created_at: "2026-01-15T10:00:00Z",
	last_seen_at: "2026-05-11T08:30:00Z",
};

describe("useModelEditor", () => {
	const mockOnUpdate = vi.fn();

	beforeEach(() => {
		mockOnUpdate.mockClear();
	});

	describe("initial state", () => {
		it("starts with editing=false", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.editing).toBe(false);
		});

		it("initializes editData with model values", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.editData).toEqual({
				display_name: "Test Model v1",
				context_length: "8192",
				max_output_tokens: "4096",
				input_price_per_million: "0.5",
				output_price_per_million: "1.5",
			});
		});

		it("initializes discoveredDefaults with model values", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.discoveredDefaults).toEqual({
				display_name: "Test Model",
				context_length: 8192,
				max_output_tokens: 4096,
				input_price_per_million: 0.5,
				output_price_per_million: 1.5,
			});
		});

		it("handles null/undefined values in editData", () => {
			const modelWithNulls = {
				...mockModel,
				display_name: undefined,
				context_length: null,
				max_output_tokens: null,
				input_price_per_million: null,
				output_price_per_million: null,
			} as unknown as Model;

			const { result } = renderHook(() =>
				useModelEditor({ model: modelWithNulls, onUpdate: mockOnUpdate }),
			);

			expect(result.current.editData).toEqual({
				display_name: "",
				context_length: "",
				max_output_tokens: "",
				input_price_per_million: "",
				output_price_per_million: "",
			});
		});
	});

	describe("start editing", () => {
		it("sets editing to true when setEditing(true) is called", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditing(true);
			});

			expect(result.current.editing).toBe(true);
		});
	});

	describe("edit fields", () => {
		it("updates editData when setEditData is called", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "Updated Name",
				}));
			});

			expect(result.current.editData.display_name).toBe("Updated Name");
		});

		it("updates multiple fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData({
					display_name: "New Name",
					context_length: "16384",
					max_output_tokens: "8192",
					input_price_per_million: "1.0",
					output_price_per_million: "2.0",
				});
			});

			expect(result.current.editData).toEqual({
				display_name: "New Name",
				context_length: "16384",
				max_output_tokens: "8192",
				input_price_per_million: "1.0",
				output_price_per_million: "2.0",
			});
		});
	});

	describe("getChangedFields", () => {
		it("returns empty array when no changes", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getChangedFields()).toEqual([]);
		});

		it("detects changed display_name", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "Changed Name",
				}));
			});

			expect(result.current.getChangedFields()).toContain("display_name");
		});

		it("detects changed context_length", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					context_length: "16384",
				}));
			});

			expect(result.current.getChangedFields()).toContain("context_length");
		});

		it("detects changed max_output_tokens", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					max_output_tokens: "8192",
				}));
			});

			expect(result.current.getChangedFields()).toContain("max_output_tokens");
		});

		it("detects changed input_price_per_million", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					input_price_per_million: "1.0",
				}));
			});

			expect(result.current.getChangedFields()).toContain(
				"input_price_per_million",
			);
		});

		it("detects changed output_price_per_million", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					output_price_per_million: "2.0",
				}));
			});

			expect(result.current.getChangedFields()).toContain(
				"output_price_per_million",
			);
		});

		it("detects multiple changed fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData({
					display_name: "Changed",
					context_length: "16384",
					max_output_tokens: "8192",
					input_price_per_million: "1.0",
					output_price_per_million: "2.0",
				});
			});

			const changed = result.current.getChangedFields();
			expect(changed).toHaveLength(5);
			expect(changed).toEqual([
				"display_name",
				"context_length",
				"max_output_tokens",
				"input_price_per_million",
				"output_price_per_million",
			]);
		});

		it("handles empty string as null for numeric fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					context_length: "",
				}));
			});

			expect(result.current.getChangedFields()).toContain("context_length");
		});
	});

	describe("getFieldLabel", () => {
		it("returns correct label for display_name", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("display_name")).toBe("Display Name");
		});

		it("returns correct label for context_length", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("context_length")).toBe(
				"Context Length",
			);
		});

		it("returns correct label for max_output_tokens", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("max_output_tokens")).toBe(
				"Max Output Tokens",
			);
		});

		it("returns correct label for input_price_per_million", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("input_price_per_million")).toBe(
				"Input Price",
			);
		});

		it("returns correct label for output_price_per_million", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("output_price_per_million")).toBe(
				"Output Price",
			);
		});

		it("returns key as fallback for unknown fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			expect(result.current.getFieldLabel("unknown_field" as never)).toBe(
				"unknown_field",
			);
		});
	});

	describe("handleCancelEdit", () => {
		it("sets confirmFields when there are changes", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "Changed",
				}));
			});

			act(() => {
				result.current.handleCancelEdit();
			});

			expect(result.current.confirmFields).toEqual(["Display Name"]);
			// editing stays unchanged when there are changes (dialog shown)
			expect(result.current.editing).toBe(false); // was false initially
		});

		it("sets editing to false when no changes", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.handleCancelEdit();
			});

			expect(result.current.confirmFields).toBeNull();
			expect(result.current.editing).toBe(false);
		});

		it("keeps editing true when canceling with changes while editing", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			// Start editing
			act(() => {
				result.current.setEditing(true);
			});

			// Make changes
			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "Changed",
				}));
			});

			// Try to cancel
			act(() => {
				result.current.handleCancelEdit();
			});

			expect(result.current.confirmFields).toEqual(["Display Name"]);
			expect(result.current.editing).toBe(true); // still editing, dialog shown
		});
	});

	describe("handleSave", () => {
		it("does nothing when no changes", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.handleSave();
			});

			expect(mockOnUpdate).not.toHaveBeenCalled();
			expect(result.current.editing).toBe(false);
		});

		it("calls onUpdate with changed fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData({
					display_name: "New Name",
					context_length: "16384",
					max_output_tokens: "8192",
					input_price_per_million: "1.0",
					output_price_per_million: "2.0",
				});
			});

			act(() => {
				result.current.handleSave();
			});

			expect(mockOnUpdate).toHaveBeenCalledWith("model-001", {
				display_name: "New Name",
				context_length: 16384,
				max_output_tokens: 8192,
				input_price_per_million: 1.0,
				output_price_per_million: 2.0,
			});
			expect(result.current.editing).toBe(false);
		});

		it("saves only changed fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "New Name",
					context_length: "8192", // unchanged
				}));
			});

			act(() => {
				result.current.handleSave();
			});

			expect(mockOnUpdate).toHaveBeenCalledWith("model-001", {
				display_name: "New Name",
			});
		});

		it("handles empty string as null for numeric fields", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					context_length: "",
				}));
			});

			act(() => {
				result.current.handleSave();
			});

			expect(mockOnUpdate).toHaveBeenCalledWith("model-001", {
				context_length: null,
			});
		});

		it("trims display_name before saving", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "  Trimmed Name  ",
				}));
			});

			act(() => {
				result.current.handleSave();
			});

			expect(mockOnUpdate).toHaveBeenCalledWith("model-001", {
				display_name: "Trimmed Name",
			});
		});
	});

	describe("revertField", () => {
		it("reverts display_name to discoveredDefaults", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					display_name: "Changed Name",
				}));
				result.current.revertField("display_name");
			});

			expect(result.current.editData.display_name).toBe("Test Model");
		});

		it("reverts context_length to discoveredDefaults", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					context_length: "16384",
				}));
				result.current.revertField("context_length");
			});

			expect(result.current.editData.context_length).toBe("8192");
		});

		it("reverts max_output_tokens to discoveredDefaults", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					max_output_tokens: "8192",
				}));
				result.current.revertField("max_output_tokens");
			});

			expect(result.current.editData.max_output_tokens).toBe("4096");
		});

		it("reverts input_price_per_million to discoveredDefaults", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					input_price_per_million: "2.0",
				}));
				result.current.revertField("input_price_per_million");
			});

			expect(result.current.editData.input_price_per_million).toBe("0.5");
		});

		it("reverts output_price_per_million to discoveredDefaults", () => {
			const { result } = renderHook(() =>
				useModelEditor({ model: mockModel, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					output_price_per_million: "3.0",
				}));
				result.current.revertField("output_price_per_million");
			});

			expect(result.current.editData.output_price_per_million).toBe("1.5");
		});

		it("handles null discoveredDefaults with empty string", () => {
			const modelWithNulls = {
				...mockModel,
				context_length: null,
			} as unknown as Model;

			const { result } = renderHook(() =>
				useModelEditor({ model: modelWithNulls, onUpdate: mockOnUpdate }),
			);

			act(() => {
				result.current.setEditData((prev) => ({
					...prev,
					context_length: "16384",
				}));
				result.current.revertField("context_length");
			});

			expect(result.current.editData.context_length).toBe("");
		});
	});

	describe("editData sync when model changes", () => {
		it("re-syncs editData when model changes while editing", () => {
			const updatedModel: Model = {
				...mockModel,
				display_name: "Updated by API",
				context_length: 16384,
			};

			const { result, rerender } = renderHook(
				({ model }) => useModelEditor({ model, onUpdate: mockOnUpdate }),
				{ initialProps: { model: mockModel } },
			);

			result.current.setEditing(true);
			result.current.setEditData((prev) => ({
				...prev,
				display_name: "Local Change",
			}));

			rerender({ model: updatedModel });

			expect(result.current.editData.display_name).toBe("Updated by API");
			expect(result.current.editData.context_length).toBe("16384");
		});

		it("does not sync when not editing", () => {
			const updatedModel: Model = {
				...mockModel,
				display_name: "Updated by API",
			};

			const { result, rerender } = renderHook(
				({ model }) => useModelEditor({ model, onUpdate: mockOnUpdate }),
				{ initialProps: { model: mockModel } },
			);

			// Not editing
			rerender({ model: updatedModel });

			expect(result.current.editData.display_name).toBe("Test Model v1");
		});
	});
});
