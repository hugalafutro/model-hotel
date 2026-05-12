import { screen, waitFor } from "@testing-library/react";
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

	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
	});

	it("renders without crashing", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		// Use getAllByText since name appears in header and display name field
		expect(screen.getAllByText("Test Model v1")).toHaveLength(2);
		expect(screen.getByText("Test Provider")).toBeInTheDocument();
		expect(screen.getByText(/8,192 tokens/)).toBeInTheDocument();
		expect(screen.getByText(/4,096 tokens/)).toBeInTheDocument();
	});

	it("displays model header with name and proxy ID", () => {
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		// Proxy ID pill contains the model ID (with hyphen in provider name)
		expect(screen.getByText("Test-Provider/test-model-v1")).toBeInTheDocument();
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
			duration_ms: 2000,
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
								duration_ms: 2000,
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
			duration_ms: 0,
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

		expect(screen.getByText("cURL")).toHaveClass("bg-slate-700/60");
		expect(screen.getByText(/curl -X POST/)).toBeInTheDocument();
	});

	it("switches to ZED snippet tab when clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("ZED"));

		expect(screen.getByText("ZED")).toHaveClass("bg-slate-700/60");
		expect(screen.getByText(/"name":/)).toBeInTheDocument();
	});

	it("copies snippet to clipboard when Copy button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailModal {...defaultProps} />);

		await user.click(screen.getByText("Copy"));

		expect(onToast).toHaveBeenCalledWith("Copied to clipboard", "info");
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
});
