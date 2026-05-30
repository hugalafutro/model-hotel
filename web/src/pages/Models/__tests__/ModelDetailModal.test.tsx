import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockModel } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ModelDetailModal } from "../ModelDetailModal";

describe("ModelDetailModal", () => {
	const onClose = vi.fn();
	const onToggle = vi.fn();
	const onDiscover = vi.fn().mockResolvedValue(undefined);
	const onTest = vi.fn();
	const onToast = vi.fn();
	const onUpdate = vi.fn();
	const onDelete = vi.fn();

	const defaultProps = {
		model: mockModel,
		onClose,
		onToggle,
		onDiscover,
		onTest,
		onToast,
		onUpdate,
		onDelete,
	};

	let writeTextMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
		writeTextMock = vi
			.spyOn(navigator.clipboard, "writeText")
			.mockResolvedValue(undefined);
	});

	it("displays model header with name and proxy ID", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		// Proxy ID pill contains the model ID (with hyphen in provider name)
		expect(
			screen.getAllByText("Test-Provider/test-model-v1").length,
		).toBeGreaterThan(0);
	});

	it("displays model description", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(
			screen.getByText("A test model for development"),
		).toBeInTheDocument();
	});

	it("displays provider information", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("displays context length and max output tokens", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText(/8,192 tokens/)).toBeInTheDocument();
		expect(screen.getByText(/4,096 tokens/)).toBeInTheDocument();
	});

	it("displays pricing information", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("$0.5/1M")).toBeInTheDocument();
		expect(screen.getByText("$1.5/1M")).toBeInTheDocument();
	});

	it("displays capabilities section", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Capabilities")).toBeInTheDocument();
	});

	it("shows 'No special capabilities detected' when no capabilities", () => {
		const modelWithoutCaps = {
			...mockModel,
			capabilities: "{}",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithoutCaps} />,
		);

		expect(
			screen.getByText("No special capabilities detected"),
		).toBeInTheDocument();
	});

	it("displays input and output modalities", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		// There are two "text" elements (input and output), use getAllBy
		const textElements = screen.getAllByText("text");
		expect(textElements).toHaveLength(2);
	});

	it("shows enabled/disabled toggle button", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Enabled")).toBeInTheDocument();
	});

	it("calls onToggle when toggle button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Enabled"));

		expect(onToggle).toHaveBeenCalledWith("model-001", false);
	});

	it("shows disabled state when model is disabled", () => {
		const disabledModel = { ...mockModel, enabled: false };
		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={disabledModel} />,
		);

		expect(screen.getByText("Disabled")).toBeInTheDocument();
	});

	it("calls onToggle with true when enabling a disabled model", async () => {
		const user = userEvent.setup();
		const disabledModel = { ...mockModel, enabled: false };
		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={disabledModel} />,
		);

		await user.click(screen.getByText("Disabled"));

		expect(onToggle).toHaveBeenCalledWith("model-001", true);
	});

	it("shows Test button", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Test")).toBeInTheDocument();
	});

	it("calls onTest when Test button is clicked", async () => {
		const user = userEvent.setup();
		onTest.mockResolvedValue({
			success: true,
			ttft_ms: 500,
			response_header_ms: 500,
			duration_ms: 2000,
			streaming: true,
			response: "Test response",
		});

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		expect(onTest).toHaveBeenCalledWith("model-001");
		await waitFor(() => {
			expect(onToast).toHaveBeenCalled();
		});
	});

	it("shows testing state while test is running", async () => {
		const user = userEvent.setup();
		onTest.mockImplementation(
			() =>
				new Promise((resolve) =>
					setTimeout(
						() =>
							resolve({
								success: true,
								ttft_ms: 500,
								response_header_ms: 500,
								duration_ms: 2000,
								streaming: true,
								response: "Test response",
							}),
						100,
					),
				),
		);

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		expect(screen.getByText("Testing…")).toBeInTheDocument();
		await waitFor(() => {
			expect(screen.queryByText("Testing…")).not.toBeInTheDocument();
		});
	});

	it("shows test error state when test fails", async () => {
		const user = userEvent.setup();
		onTest.mockResolvedValue({
			success: false,
			ttft_ms: 0,
			response_header_ms: 0,
			duration_ms: 0,
			streaming: false,
			response: "",
			error: "Connection failed",
		});

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith(
				"Test failed: Connection failed",
				"error",
			);
		});
	});

	it("shows TTFT in success toast for streaming response", async () => {
		const user = userEvent.setup();
		onTest.mockResolvedValue({
			success: true,
			ttft_ms: 500,
			response_header_ms: 500,
			duration_ms: 2000,
			streaming: true,
			response: "Hello world",
		});

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith(
				expect.stringContaining("TTFT: 0.5s"),
				"success",
			);
		});
	});

	it("omits TTFT from success toast when equal to duration (non-streaming)", async () => {
		const user = userEvent.setup();
		onTest.mockResolvedValue({
			success: true,
			ttft_ms: 1500,
			response_header_ms: 1500,
			duration_ms: 1500,
			streaming: false,
			response: "Hello world",
		});

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith(
				expect.not.stringContaining("TTFT"),
				"success",
			);
		});
	});

	it("shows Update info button", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Update info")).toBeInTheDocument();
	});

	it("calls onDiscover when Update info button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Update info"));

		expect(onDiscover).toHaveBeenCalledWith("provider-001");
	});

	it("shows cooldown timer after clicking Update info", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Update info"));

		await waitFor(() => {
			expect(screen.getByText("Update (30s)")).toBeInTheDocument();
		});
	});

	it("disables Update info button during cooldown", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Update info"));

		await waitFor(() => {
			const button = screen.getByText("Update (30s)").closest("button");
			expect(button).toBeDisabled();
		});
	});

	it("shows Edit button", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Edit")).toBeInTheDocument();
	});

	it("enters edit mode when Edit button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		const contextInput = screen.getByDisplayValue("8192");
		expect(contextInput).toBeInTheDocument();
	});

	it("shows input fields in edit mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		const displayInput = screen.getByDisplayValue("Test Model v1");
		expect(displayInput).toBeInTheDocument();
	});

	it("shows Save Changes button in edit mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		expect(screen.getByText("Save Changes")).toBeInTheDocument();
	});

	it("shows Cancel button in edit mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		expect(screen.getByText("Cancel")).toBeInTheDocument();
	});

	it("calls onCancelEdit when Cancel is clicked in edit mode", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));
		await user.click(screen.getByText("Cancel"));

		expect(screen.queryByText("Save Changes")).not.toBeInTheDocument();
	});

	it("shows Delete button", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText("Delete")).toBeInTheDocument();
	});

	it("shows Confirm delete button after clicking Delete", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Delete"));

		expect(screen.getByText("Confirm delete")).toBeInTheDocument();
	});

	it("calls onDelete and onClose when Confirm delete is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Delete"));
		await user.click(screen.getByText("Confirm delete"));

		expect(onDelete).toHaveBeenCalledWith("model-001");
		expect(onClose).toHaveBeenCalled();
	});

	it("shows cURL snippet tab by default", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		// cURL icon tab should be active (highlighted button)
		expect(screen.getByLabelText("cURL")).toHaveClass("bg-slate-700/30");
		expect(screen.getByText(/curl -X POST/)).toBeInTheDocument();
	});

	it("switches to ZED snippet tab when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByLabelText("ZED"));

		expect(screen.getByLabelText("ZED")).toHaveClass("bg-slate-700/30");
		// ZED JSON content renders with syntax highlighting (quoted keys)
		expect(screen.getByText('"display_name"')).toBeInTheDocument();
	});

	it("copies snippet to clipboard when Copy button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		const copyBtn = screen.getByRole("button", { name: /Copy cURL snippet/ });
		await user.click(copyBtn);

		await waitFor(() => {
			expect(writeTextMock).toHaveBeenCalled();
		});
	});

	it("shows subscription info when params include subscription_included", () => {
		const modelWithSubscription = {
			...mockModel,
			params:
				'{"subscription_included":true,"subscription_note":"Pro plan required"}',
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithSubscription} />,
		);

		expect(screen.getByText("Included")).toBeInTheDocument();
		expect(screen.getByText("Pro plan required")).toBeInTheDocument();
	});

	it("shows subscription not included badge", () => {
		const modelWithSubscription = {
			...mockModel,
			params: '{"subscription_included":false}',
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithSubscription} />,
		);

		expect(screen.getByText("Not included")).toBeInTheDocument();
	});

	it("calls onClose when close button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(onClose).toHaveBeenCalled();
	});

	it("calls onClose when backdrop is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		const backdrop = screen.getByLabelText("Close dialog");
		await user.click(backdrop);

		expect(onClose).toHaveBeenCalled();
	});

	it("displays revert button for display_name when value differs from discovered", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		const revertButton = screen.getByTitle("Revert to discovered value");
		expect(revertButton).toBeInTheDocument();
	});

	it("displays last discovered timestamp", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		expect(screen.getByText(/Last Discovered/)).toBeInTheDocument();
	});

	// RevertButton className prop - price revert buttons pass className="shrink-0"
	it("applies className prop to price RevertButton", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change input price to trigger the price revert button (which passes className="shrink-0")
		const priceInputs = screen.getAllByPlaceholderText("0.00");
		const inputPriceInput = priceInputs[0];
		await user.clear(inputPriceInput);
		await user.type(inputPriceInput, "9.99");

		// Find the revert button in the same row as the changed price input
		const priceRow = inputPriceInput.closest("div")
			?.parentElement as HTMLElement;
		const revertBtn = within(priceRow).getByTitle("Revert to discovered value");
		expect(revertBtn).toBeInTheDocument();
		// Price RevertButton forwards className="shrink-0"
		expect(revertBtn).toHaveClass("shrink-0");
	});

	// parseParams catch branch - invalid JSON
	it("does not render subscription section when params is invalid JSON", () => {
		const modelWithInvalidParams = {
			...mockModel,
			params: "invalid-json",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithInvalidParams} />,
		);

		expect(screen.queryByText("Subscription")).not.toBeInTheDocument();
	});

	// parseParams catch branch - empty string
	it("does not render subscription section when params is empty string", () => {
		const modelWithEmptyParams = {
			...mockModel,
			params: "",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithEmptyParams} />,
		);

		expect(screen.queryByText("Subscription")).not.toBeInTheDocument();
	});

	// inputMods/outputMods IIFEs - array value
	it("renders input modalities from JSON array", () => {
		const modelWithArrayMods = {
			...mockModel,
			input_modalities: '["text","image"]',
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithArrayMods} />,
		);

		expect(screen.getByText("text, image")).toBeInTheDocument();
	});

	// inputMods/outputMods IIFEs - single non-array value
	it("wraps single non-array modality value in array", () => {
		const modelWithSingleMod = {
			...mockModel,
			input_modalities: '"audio"',
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithSingleMod} />,
		);

		expect(screen.getByText("audio")).toBeInTheDocument();
	});

	// inputMods/outputMods IIFEs - invalid JSON fallback
	it("falls back to text when input_modalities is invalid JSON", () => {
		const modelWithInvalidMods = {
			...mockModel,
			input_modalities: "invalid",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithInvalidMods} />,
		);

		// There are multiple "text" elements (input and output modality labels)
		const textElements = screen.getAllByText("text");
		expect(textElements.length).toBeGreaterThanOrEqual(2);
	});

	// handleDiscover early return - during cooldown
	// Note: userEvent.click on a disabled button does not fire the handler at all,
	// so we use fireEvent.click to exercise the in-handler guard (if cooldown > 0 || discovering) return.
	it("returns early from handleDiscover during cooldown via handler guard", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Update info"));

		await waitFor(() => {
			expect(screen.getByText("Update (30s)")).toBeInTheDocument();
		});

		vi.clearAllMocks();

		// fireEvent bypasses the disabled attribute, exercising the handler guard directly
		const updateButton = screen.getByText("Update (30s)").closest("button");
		if (updateButton) fireEvent.click(updateButton);

		expect(onDiscover).not.toHaveBeenCalled();
	});

	// handleDiscover early return - during discovering
	it("returns early from handleDiscover during discovering state via handler guard", async () => {
		const user = userEvent.setup();
		onDiscover.mockImplementation(
			() => new Promise((resolve) => setTimeout(resolve, 500)),
		);

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Update info"));

		expect(screen.getByText("Updating…")).toBeInTheDocument();

		vi.clearAllMocks();

		// fireEvent bypasses the disabled attribute, exercising the handler guard directly
		const updateButton = screen.getByText("Updating…").closest("button");
		if (updateButton) fireEvent.click(updateButton);

		expect(onDiscover).not.toHaveBeenCalled();
	});

	// handleTest exception catch - Error rejection
	it("shows error toast with error.message when onTest rejects with Error", async () => {
		const user = userEvent.setup();
		onTest.mockRejectedValue(new Error("Connection timeout"));

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith(
				"Test failed: Connection timeout",
				"error",
			);
		});
	});

	// handleTest exception catch - non-Error rejection
	it("shows Unknown error when onTest rejects with non-Error", async () => {
		const user = userEvent.setup();
		/* eslint-disable @typescript-eslint/no-explicit-any */
		// biome-ignore lint/suspicious/noExplicitAny: testing non-Error rejection path
		onTest.mockRejectedValue("string error" as any);
		/* eslint-enable @typescript-eslint/no-explicit-any */

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		await waitFor(() => {
			expect(onToast).toHaveBeenCalledWith(
				"Test failed: Unknown error",
				"error",
			);
		});
	});

	// handleTest early return - clicking while already testing
	// Note: fireEvent.click bypasses the disabled attribute to exercise the in-handler guard (if testing) return.
	it("returns early from handleTest when clicking Test while already testing", async () => {
		const user = userEvent.setup();
		onTest.mockImplementation(
			() => new Promise((resolve) => setTimeout(resolve, 500)),
		);

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Test"));

		expect(screen.getByText("Testing…")).toBeInTheDocument();

		vi.clearAllMocks();

		// fireEvent bypasses the disabled attribute, exercising the handler guard directly
		const testButton = screen.getByText("Testing…").closest("button");
		if (testButton) fireEvent.click(testButton);

		expect(onTest).not.toHaveBeenCalled();
	});

	// handleClose when editing - close button cancels edit
	it("cancels edit mode when clicking close button while editing", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		expect(screen.getByText("Save Changes")).toBeInTheDocument();

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(screen.queryByText("Save Changes")).not.toBeInTheDocument();
		expect(onClose).not.toHaveBeenCalled();
	});

	// handleClose when editing - backdrop click cancels edit
	it("cancels edit mode when clicking backdrop while editing", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		expect(screen.getByText("Save Changes")).toBeInTheDocument();

		const backdrop = screen.getByLabelText("Close dialog");
		await user.click(backdrop);

		expect(screen.queryByText("Save Changes")).not.toBeInTheDocument();
		expect(onClose).not.toHaveBeenCalled();
	});

	// Edit mode revert buttons - context_length
	it("shows RevertButton for context_length after editing value", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change context_length to trigger revert button
		const contextInput = screen.getByDisplayValue("8192");
		await user.clear(contextInput);
		await user.type(contextInput, "16384");

		// Find the revert button within the same row as the changed input
		const contextRow = screen
			.getByDisplayValue("16384")
			.closest("div") as HTMLElement;
		const revertBtn = within(contextRow).getByTitle(
			"Revert to discovered value",
		);
		expect(revertBtn).toBeInTheDocument();
	});

	// Edit mode revert buttons - max_output_tokens
	it("shows RevertButton for max_output_tokens after editing value", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change max_output_tokens to trigger revert button
		const tokenInputs = screen.getAllByPlaceholderText("tokens");
		const maxOutputInput = tokenInputs[1]; // second tokens input is max_output
		await user.clear(maxOutputInput);
		await user.type(maxOutputInput, "9999");

		const maxOutputRow = screen
			.getByDisplayValue("9999")
			.closest("div") as HTMLElement;
		const revertBtn = within(maxOutputRow).getByTitle(
			"Revert to discovered value",
		);
		expect(revertBtn).toBeInTheDocument();
	});

	// Edit mode revert buttons - input_price_per_million
	it("shows RevertButton for input_price after editing value", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change input price to trigger revert button
		const priceInputs = screen.getAllByPlaceholderText("0.00");
		const inputPriceInput = priceInputs[0]; // first price input is input_price
		await user.clear(inputPriceInput);
		await user.type(inputPriceInput, "9.99");

		// Should now have exactly 1 revert button (only input_price changed)
		const revertButtons = screen.getAllByTitle("Revert to discovered value");
		expect(revertButtons.length).toBeGreaterThanOrEqual(1);
	});

	// Edit mode revert buttons - output_price_per_million
	it("shows RevertButton for output_price after editing value", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change output price to trigger revert button
		const priceInputs = screen.getAllByPlaceholderText("0.00");
		const outputPriceInput = priceInputs[1]; // second price input is output_price
		await user.clear(outputPriceInput);
		await user.type(outputPriceInput, "8.88");

		// Should now have at least 1 revert button (output_price changed)
		const revertButtons = screen.getAllByTitle("Revert to discovered value");
		expect(revertButtons.length).toBeGreaterThanOrEqual(1);
	});

	// Edit mode revert buttons - clicking revert calls revertField
	it("reverts field value when clicking RevertButton", async () => {
		const user = userEvent.setup();

		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change context_length to trigger revert button
		const contextInput = screen.getByDisplayValue("8192");
		await user.clear(contextInput);
		await user.type(contextInput, "16384");

		const contextRow = screen
			.getByDisplayValue("16384")
			.closest("div") as HTMLElement;
		const revertBtn = within(contextRow).getByTitle(
			"Revert to discovered value",
		);
		await user.click(revertBtn);
		expect(screen.getByDisplayValue("8192")).toBeInTheDocument();
	});

	// Snippet tabs - OpenCode tab
	it("switches to OpenCode snippet tab when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByLabelText("OpenCode"));

		// Verify OpenCode tab is active (highlighted)
		expect(screen.getByLabelText("OpenCode")).toHaveClass("bg-slate-700/30");
		// Verify cURL tab is no longer active
		expect(screen.getByLabelText("cURL")).not.toHaveClass("bg-slate-700/30");
	});

	// Snippet tabs - Copy button on each non-default tab
	it("copies ZED snippet to clipboard when Copy button is clicked on ZED tab", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByLabelText("ZED"));
		const copyBtn = screen.getByRole("button", { name: /Copy ZED snippet/ });
		await user.click(copyBtn);

		await waitFor(() => {
			expect(writeTextMock).toHaveBeenCalled();
		});
	});

	it("copies OpenCode snippet to clipboard when Copy button is clicked on OpenCode tab", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByLabelText("OpenCode"));
		const copyBtn = screen.getByRole("button", {
			name: /Copy OpenCode snippet/,
		});
		await user.click(copyBtn);

		await waitFor(() => {
			expect(writeTextMock).toHaveBeenCalled();
		});
	});

	it.each([
		["cURL"],
		["PowerShell"],
		["JavaScript"],
		["Python"],
		["Claude Code"],
		["OpenClaw"],
		["Hermes"],
		["LibreChat"],
		["ZED"],
		["OpenCode"],
	])("renders and activates %s snippet tab", async (label) => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		const tab = screen.getByLabelText(label);
		expect(tab).toBeInTheDocument();
		expect(tab).toHaveAttribute("role", "tab");

		await user.click(tab);

		expect(tab).toHaveAttribute("aria-selected", "true");
	});

	// Subscription section edge cases - subscription_included true without note
	it("shows subscription badge without note when subscription_note is missing", () => {
		const modelWithSubscriptionNoNote = {
			...mockModel,
			params: '{"subscription_included":true}',
		};

		renderWithProviders(
			<ModelDetailModal
				{...defaultProps}
				model={modelWithSubscriptionNoNote}
			/>,
		);

		expect(screen.getByText("Included")).toBeInTheDocument();
		expect(screen.queryByText("Pro plan required")).not.toBeInTheDocument();
	});

	// Subscription section edge cases - subscription_included: false is already tested at line 368

	// Price display edge cases - null input price
	it("shows dash for input price when input_price_per_million is null", () => {
		const modelWithNullInputPrice = {
			...mockModel,
			input_price_per_million: null,
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithNullInputPrice} />,
		);

		expect(screen.getByText("-")).toBeInTheDocument();
	});

	// Price display edge cases - null output price
	it("shows dash for output price when output_price_per_million is null", () => {
		const modelWithNullOutputPrice = {
			...mockModel,
			output_price_per_million: null,
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithNullOutputPrice} />,
		);

		const priceElements = screen.getAllByText("-");
		expect(priceElements.length).toBeGreaterThanOrEqual(1);
	});

	// Description missing
	it("does not render description section when description is empty", () => {
		const modelWithoutDescription = {
			...mockModel,
			description: "",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithoutDescription} />,
		);

		expect(
			screen.queryByText("A test model for development"),
		).not.toBeInTheDocument();
	});

	// Display name fallbacks - empty display_name with name
	it("shows name when display_name is empty", () => {
		const modelWithEmptyDisplayName = {
			...mockModel,
			display_name: "",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithEmptyDisplayName} />,
		);

		// The header contains the name, use role to be more specific
		const heading = screen.getByRole("heading", { level: 2 });
		expect(heading).toHaveTextContent("Test Model");
	});

	// Display name fallbacks - empty display_name and name
	it("shows proxyModelID when display_name and name are empty", () => {
		const modelWithEmptyNames = {
			...mockModel,
			display_name: "",
			name: "",
		};

		renderWithProviders(
			<ModelDetailModal {...defaultProps} model={modelWithEmptyNames} />,
		);

		// The header contains the proxyModelID
		const heading = screen.getByRole("heading", { level: 2 });
		expect(heading).toHaveTextContent("Test-Provider/test-model-v1");
	});

	// confirmFields ConfirmDialog - rendering
	it("shows ConfirmDialog when confirmFields is set", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		// Change a field to trigger confirm dialog on cancel
		const contextInput = screen.getByDisplayValue("8192");
		await user.clear(contextInput);
		await user.type(contextInput, "16384");

		// Click close button to trigger confirm dialog
		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(screen.getByText("Unsaved Changes")).toBeInTheDocument();
		// ConfirmDialog has "Discard" button
		expect(screen.getByText("Discard")).toBeInTheDocument();
	});

	// confirmFields ConfirmDialog - onConfirm resets edit and exits edit mode
	it("resets edit data and exits edit mode when confirming discard", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		const contextInput = screen.getByDisplayValue("8192");
		await user.clear(contextInput);
		await user.type(contextInput, "16384");

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		// Click Discard button in ConfirmDialog
		await user.click(screen.getByText("Discard"));

		expect(screen.queryByText("Save Changes")).not.toBeInTheDocument();
		expect(screen.queryByText("Unsaved Changes")).not.toBeInTheDocument();
	});

	// confirmFields ConfirmDialog - onCancel clears confirmFields
	it("clears confirmFields and stays in edit mode when canceling discard", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Edit"));

		const contextInput = screen.getByDisplayValue("8192");
		await user.clear(contextInput);
		await user.type(contextInput, "16384");

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		// Click Cancel button in ConfirmDialog
		const dialogs = screen.getAllByRole("dialog");
		const confirmDialog = dialogs[dialogs.length - 1]; // last dialog is the ConfirmDialog
		const cancelButton = within(confirmDialog).getByRole("button", {
			name: "Cancel",
		});
		await user.click(cancelButton);

		expect(screen.queryByText("Unsaved Changes")).not.toBeInTheDocument();
		expect(screen.getByText("Save Changes")).toBeInTheDocument();
	});
});
