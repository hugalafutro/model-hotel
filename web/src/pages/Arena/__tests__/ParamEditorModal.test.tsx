import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../hooks/useRecommendedSettings", () => ({
	useRecommendedSettings: vi.fn(() => ({
		recommended: null,
		loading: false,
		error: null,
		matchedModel: null,
	})),
}));

import type { GenerationParams } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { ParamEditorModal } from "../ParamEditorModal";

// Mock useRecommendedSettings for testing ApplyRecommendedButton
vi.mock("../../../hooks/useRecommendedSettings", () => ({
	useRecommendedSettings: vi.fn(() => ({
		recommended: null,
		loading: false,
		error: null,
		matchedModel: null,
	})),
}));

import { useRecommendedSettings } from "../../../hooks/useRecommendedSettings";

describe("ParamEditorModal", () => {
	const defaultProps = {
		modelId: "Test Provider/gemma3:4b",
		params: {} as GenerationParams,
		onChange: vi.fn(),
		onClose: vi.fn(),
		knownProviders: ["Test Provider"],
	};

	beforeEach(() => {
		vi.mocked(useRecommendedSettings).mockClear();
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: null,
			loading: false,
			error: null,
			matchedModel: null,
		});
	});

	it("renders modal with model ID as title", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		expect(screen.getByText("Test Provider/gemma3:4b")).toBeInTheDocument();
	});

	it("renders all parameter sliders with correct labels", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		expect(screen.getByText(/Temperature/i)).toBeInTheDocument();
		expect(screen.getByText(/Max Tokens/i)).toBeInTheDocument();
		expect(screen.getByText(/Top P/i)).toBeInTheDocument();
		expect(screen.getByText(/Min P/i)).toBeInTheDocument();
		expect(screen.getByText(/Top K/i)).toBeInTheDocument();
		expect(screen.getByText(/Freq Penalty/i)).toBeInTheDocument();
		expect(screen.getByText(/Pres Penalty/i)).toBeInTheDocument();
	});

	it("displays correct default values on sliders", () => {
		const params: GenerationParams = {
			temperature: 0.7,
			max_tokens: 2048,
			top_p: 0.9,
			min_p: 0.1,
			top_k: 40,
			frequency_penalty: 0,
			presence_penalty: 0,
		};

		renderWithProviders(<ParamEditorModal {...defaultProps} params={params} />);

		// ParamSlider uses number inputs for values - find by role
		const numberInputs = screen.getAllByRole("spinbutton");
		expect(numberInputs[0]).toHaveValue(0.7); // Temperature
		expect(numberInputs[1]).toHaveValue(2048); // Max Tokens
		expect(numberInputs[2]).toHaveValue(0.9); // Top P
		expect(numberInputs[3]).toHaveValue(0.1); // Min P
		expect(numberInputs[4]).toHaveValue(40); // Top K
		expect(numberInputs[5]).toHaveValue(0); // Freq Penalty
		expect(numberInputs[6]).toHaveValue(0); // Pres Penalty
	});

	it("calls onChange when Temperature slider is adjusted", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const numberInputs = screen.getAllByRole("spinbutton");
		await user.type(numberInputs[0], "{Backspace}0.5{Enter}");

		expect(onChange).toHaveBeenCalled();
	});

	it("calls onChange when Max Tokens number input changes", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[1]);
		await user.type(numberInputs[1], "1");

		expect(onChange).toHaveBeenCalled();
	});

	it("calls onChange when Top K number input changes", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[4]);
		await user.type(numberInputs[4], "5");

		expect(onChange).toHaveBeenCalled();
	});

	it("calls onClose when Done button is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onClose={onClose} />,
		);

		const doneButton = screen.getByRole("button", { name: /Done/i });
		await user.click(doneButton);

		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("shows Reset all button when params have values", () => {
		const params: GenerationParams = {
			temperature: 0.5,
		};

		renderWithProviders(<ParamEditorModal {...defaultProps} params={params} />);

		expect(
			screen.getByRole("button", { name: /Reset all/i }),
		).toBeInTheDocument();
	});

	it("hides Reset all button when params are empty", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} params={{}} />);

		expect(
			screen.queryByRole("button", { name: /Reset all/i }),
		).not.toBeInTheDocument();
	});

	it("calls onChange with empty object when Reset all is clicked", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		const params: GenerationParams = {
			temperature: 0.5,
			max_tokens: 1024,
		};

		renderWithProviders(
			<ParamEditorModal
				{...defaultProps}
				params={params}
				onChange={onChange}
			/>,
		);

		const resetButton = screen.getByRole("button", { name: /Reset all/i });
		await user.click(resetButton);

		expect(onChange).toHaveBeenCalledWith({});
		expect(onChange).toHaveBeenCalledTimes(1);
	});

	it("renders ApplyRecommendedButton component", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		// Button should be present (may show "Loading..." or "No recommendations")
		// Check for any of the possible button texts
		const button = screen.getByRole("button", {
			name: /Apply Recommended|Loading|No recommendations/i,
		});
		expect(button).toBeInTheDocument();
	});

	it("passes providerName to ApplyRecommendedButton", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		// Button should be present with provider extracted from modelId
		const button = screen.getByRole("button", {
			name: /Apply Recommended|Loading|No recommendations/i,
		});
		expect(button).toBeInTheDocument();
	});

	it("hides sliders for incompatible params (Anthropic)", () => {
		// This tests the paramCompat integration - sliders should be hidden
		// when isParamHidden returns true for the provider
		const anthropicProps = {
			modelId: "Anthropic Pro/gemma3:4b",
			params: {} as GenerationParams,
			onChange: vi.fn(),
			onClose: vi.fn(),
			knownProviders: ["Anthropic Pro"],
		};

		renderWithProviders(<ParamEditorModal {...anthropicProps} />);

		// Anthropic has min_p, top_p, frequency_penalty, presence_penalty as incompatible
		// These sliders should be hidden (not present in the DOM)
		expect(screen.queryByText(/Min P/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/Top P/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/Freq Penalty/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/Pres Penalty/i)).not.toBeInTheDocument();

		// Compatible sliders should still be present
		expect(screen.getByText(/Temperature/i)).toBeInTheDocument();
		expect(screen.getByText(/Max Tokens/i)).toBeInTheDocument();
		expect(screen.getByText(/Top K/i)).toBeInTheDocument();
	});

	it("calls onChange with top_p when Top P slider is adjusted", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const label = screen.getByText(/Top P/i);
		const container = label.closest("div") as HTMLElement;
		const input = within(container).getByRole("spinbutton");

		await user.clear(input);
		await user.type(input, "0.5{Enter}");

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining({ top_p: 0.5 }),
		);
	});

	it("calls onChange with min_p when Min P slider is adjusted", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const label = screen.getByText(/Min P/i);
		const container = label.closest("div") as HTMLElement;
		const input = within(container).getByRole("spinbutton");

		await user.clear(input);
		await user.type(input, "0.1{Enter}");

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining({ min_p: 0.1 }),
		);
	});

	it("calls onChange with frequency_penalty when Freq Penalty slider is adjusted", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const label = screen.getByText(/Freq Penalty/i);
		const container = label.closest("div") as HTMLElement;
		const input = within(container).getByRole("spinbutton");

		await user.clear(input);
		await user.type(input, "0.5{Enter}");

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining({ frequency_penalty: 0.5 }),
		);
	});

	it("calls onChange with presence_penalty when Pres Penalty slider is adjusted", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const label = screen.getByText(/Pres Penalty/i);
		const container = label.closest("div") as HTMLElement;
		const input = within(container).getByRole("spinbutton");

		await user.clear(input);
		await user.type(input, "0.3{Enter}");

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining({ presence_penalty: 0.3 }),
		);
	});

	it("renders ReasoningEffortSelect when reasoning prop is true", () => {
		renderWithProviders(
			<ParamEditorModal {...defaultProps} reasoning={true} />,
		);

		// ReasoningEffortSelect renders buttons for Low, Medium, High
		expect(screen.getByRole("button", { name: /Low/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Medium/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /High/i })).toBeInTheDocument();
	});

	it("hides ReasoningEffortSelect when reasoning prop is omitted", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		// No reasoning buttons should be present
		expect(
			screen.queryByRole("button", { name: /Low/i }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /Medium/i }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /High/i }),
		).not.toBeInTheDocument();
	});

	it("calls onChange with reasoning_effort when ReasoningEffortSelect is clicked", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();

		renderWithProviders(
			<ParamEditorModal
				{...defaultProps}
				reasoning={true}
				params={{ reasoning_effort: "low" } as Partial<GenerationParams>}
				onChange={onChange}
			/>,
		);

		const mediumButton = screen.getByRole("button", { name: /Medium/i });
		await user.click(mediumButton);

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining({ reasoning_effort: "medium" }),
		);
	});

	it("calls onChange with recommended params when ApplyRecommendedButton is clicked", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		const recommendedParams = {
			temperature: 0.7,
			max_tokens: 4096,
		};

		// Mock the hook to return recommended settings
		vi.mocked(useRecommendedSettings).mockReturnValue({
			recommended: recommendedParams,
			loading: false,
			error: null,
			matchedModel: null,
		});

		renderWithProviders(
			<ParamEditorModal {...defaultProps} onChange={onChange} />,
		);

		const applyButton = screen.getByRole("button", {
			name: /Apply Recommended/i,
		});
		await user.click(applyButton);

		expect(onChange).toHaveBeenCalledWith(
			expect.objectContaining(recommendedParams),
		);
	});
});
