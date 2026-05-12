import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";

// Mock useRecommendedSettings hook
vi.mock("../../hooks/useRecommendedSettings", () => ({
	useRecommendedSettings: vi.fn(),
}));

import { useRecommendedSettings } from "../../hooks/useRecommendedSettings";
import { ApplyRecommendedButton } from "../ApplyRecommendedButton";

describe("ApplyRecommendedButton", () => {
	const onApply = vi.fn();

	beforeEach(() => {
		onApply.mockClear();
		vi.mocked(useRecommendedSettings).mockClear();
	});

	it("shows loading state", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: null,
			loading: true,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("Loading…")).toBeInTheDocument();
		expect(screen.getByRole("button")).toBeDisabled();
	});

	it("shows 'No recommendations available' when recommended is null", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: null,
			loading: false,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(
			screen.getByText("No recommendations available"),
		).toBeInTheDocument();
		expect(screen.getByRole("button")).toBeDisabled();
	});

	it("shows 'Apply Recommended' with param count when recommended exists", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: {
				temperature: 0.7,
				max_tokens: 4096,
				top_p: 0.9,
			},
			loading: false,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("Apply Recommended")).toBeInTheDocument();
		expect(screen.getByText("(3 params)")).toBeInTheDocument();
		expect(screen.getByRole("button")).not.toBeDisabled();
	});

	it("shows matched model badge when matchedModel differs", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: {
				temperature: 0.7,
			},
			loading: false,
			error: null,
			matchedModel: "different-model",
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(
			screen.getByTitle("models.dev matched: different-model"),
		).toBeInTheDocument();
	});

	it("calls onApply with recommended params when clicked", async () => {
		const recommendedParams = {
			temperature: 0.7,
			max_tokens: 4096,
		};

		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: recommendedParams,
			loading: false,
			error: null,
			matchedModel: null,
		});

		const user = userEvent.setup();
		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		await user.click(screen.getByRole("button"));

		expect(onApply).toHaveBeenCalledTimes(1);
		expect(onApply).toHaveBeenCalledWith(recommendedParams);
	});

	it("button is disabled when no recommendations", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: null,
			loading: false,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(screen.getByRole("button")).toBeDisabled();
	});

	it("button is disabled when loading", () => {
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: null,
			loading: true,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ApplyRecommendedButton
				modelId="test-model"
				providerName="Test Provider"
				onApply={onApply}
			/>,
		);

		expect(screen.getByRole("button")).toBeDisabled();
	});
});
