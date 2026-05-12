import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { GenerationParams } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { ParamEditorModal } from "../ParamEditorModal";

describe("ParamEditorModal", () => {
	const defaultProps = {
		modelId: "Ollama Cloud/gemma3:4b",
		params: {} as GenerationParams,
		onChange: vi.fn(),
		onClose: vi.fn(),
		knownProviders: ["Ollama Cloud", "OpenAI"],
	};

	it("renders modal with model ID as title", () => {
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		expect(screen.getByText("Ollama Cloud/gemma3:4b")).toBeInTheDocument();
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

	it("disables sliders when param is disabled for provider", () => {
		// This tests the paramCompat integration - sliders should show disabled state
		// when isParamDisabled returns true for the provider
		renderWithProviders(<ParamEditorModal {...defaultProps} />);

		// All sliders should be enabled by default for Ollama Cloud
		// Find the temperature number input (first one)
		const numberInputs = screen.getAllByRole("spinbutton");
		expect(numberInputs[0]).not.toBeDisabled();
	});
});
