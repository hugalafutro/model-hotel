import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ApplyRecommendedButton } from "../ApplyRecommendedButton";

describe("ApplyRecommendedButton", () => {
	it("caps a large stored max output at 4096 and applies it", async () => {
		const onApply = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<ApplyRecommendedButton
				modelId="Test Provider/test-model"
				maxOutputTokens={128000}
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("(1 params)")).toBeInTheDocument();
		await user.click(screen.getByRole("button"));
		expect(onApply).toHaveBeenCalledWith({ max_tokens: 4096 });
	});

	it("uses a stored max output below the cap as is", async () => {
		const onApply = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<ApplyRecommendedButton
				modelId="Test Provider/test-model"
				maxOutputTokens={2000}
				onApply={onApply}
			/>,
		);

		await user.click(screen.getByRole("button"));
		expect(onApply).toHaveBeenCalledWith({ max_tokens: 2000 });
	});

	it("merges curated family params with the stored max output", async () => {
		const onApply = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<ApplyRecommendedButton
				modelId="OpenAI/gpt-4o"
				maxOutputTokens={16384}
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("Apply Recommended")).toBeInTheDocument();
		expect(screen.getByText("(3 params)")).toBeInTheDocument();
		await user.click(screen.getByRole("button"));
		expect(onApply).toHaveBeenCalledWith({
			temperature: 0.7,
			top_p: 1,
			max_tokens: 4096,
		});
	});

	it("is disabled with no stored max output and no curated family", async () => {
		const onApply = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<ApplyRecommendedButton
				modelId="Test Provider/test-model"
				maxOutputTokens={null}
				onApply={onApply}
			/>,
		);

		expect(
			screen.getByText("No recommendations available"),
		).toBeInTheDocument();
		const button = screen.getByRole("button");
		expect(button).toBeDisabled();
		await user.click(button);
		expect(onApply).not.toHaveBeenCalled();
	});
});
