import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockModel } from "../../test/mocks/data";
import { renderWithProviders } from "../../test/utils";
import { ModelDetailModal, ModelDetailPanel } from "../ModelDetailPanel";

describe("ModelDetailPanel", () => {
	const defaultProps = {
		model: mockModel,
	};

	it("renders without crashing", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test Model v1")).toBeInTheDocument();
		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("displays model name as title", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test Model v1")).toBeInTheDocument();
	});

	it("displays model description", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.getByText("A test model for development"),
		).toBeInTheDocument();
	});

	it("displays provider name", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("displays model ID", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("test-model-v1")).toBeInTheDocument();
	});

	it("displays context length", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("8,192")).toBeInTheDocument();
	});

	it("displays max output tokens", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("4,096")).toBeInTheDocument();
	});

	it("displays input price", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("$0.5")).toBeInTheDocument();
	});

	it("displays output price", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("$1.5")).toBeInTheDocument();
	});

	it("displays capabilities section", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		// Capabilities section exists with badges
		expect(screen.getByText("Provider")).toBeInTheDocument();
	});

	it("displays proxy ID pill", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test-Provider/test-model-v1")).toBeInTheDocument();
	});

	it("shows collapse/expand toggle by default", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		const collapseButton = screen.getByRole("button", {
			name: /Collapse model details/i,
		});
		expect(collapseButton).toBeInTheDocument();
	});

	it("does not show collapse toggle when collapsible is false", () => {
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} collapsible={false} />,
		);

		expect(
			screen.queryByRole("button", { name: /Collapse model details/i }),
		).not.toBeInTheDocument();
	});

	it("collapses when collapse button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		await user.click(
			screen.getByRole("button", { name: /Collapse model details/i }),
		);

		expect(
			screen.queryByText("A test model for development"),
		).not.toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /Expand model details/i }),
		).toBeInTheDocument();
	});

	it("expands when expand button is clicked after collapsing", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		await user.click(
			screen.getByRole("button", { name: /Collapse model details/i }),
		);
		await user.click(
			screen.getByRole("button", { name: /Expand model details/i }),
		);

		expect(
			screen.getByText("A test model for development"),
		).toBeInTheDocument();
	});

	it("shows settings cog when params and onParamsChange are provided", () => {
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /Generation parameters/i }),
		).toBeInTheDocument();
	});

	it("does not show settings cog when params is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.queryByRole("button", { name: /Generation parameters/i }),
		).not.toBeInTheDocument();
	});

	it("does not show settings cog when onParamsChange is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} params={{}} />);

		expect(
			screen.queryByRole("button", { name: /Generation parameters/i }),
		).not.toBeInTheDocument();
	});

	it("shows parameter sliders when settings cog is clicked", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// Sliders use span labels, not aria-labels
		expect(screen.getByText("Temperature")).toBeInTheDocument();
		expect(screen.getByText("Max Tokens")).toBeInTheDocument();
		expect(screen.getByText("Top P")).toBeInTheDocument();
		expect(screen.getByText("Min P")).toBeInTheDocument();
		expect(screen.getByText("Top K")).toBeInTheDocument();
		expect(screen.getByText("Freq Penalty")).toBeInTheDocument();
		expect(screen.getByText("Pres Penalty")).toBeInTheDocument();
	});

	it("shows reset button when custom params are set", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ temperature: 0.7 }}
				onParamsChange={onParamsChange}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /Reset parameters/i }),
		).toBeInTheDocument();
	});

	it("does not show reset button when no custom params", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		expect(
			screen.queryByRole("button", { name: /Reset parameters/i }),
		).not.toBeInTheDocument();
	});

	it("calls onParamsChange when reset button is clicked", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ temperature: 0.7 }}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(screen.getByRole("button", { name: /Reset parameters/i }));

		expect(onParamsChange).toHaveBeenCalledWith({});
	});

	it("calls onParamsChange when temperature slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		// Find the number input for temperature and change it
		const numberInputs = screen.getAllByRole("spinbutton");
		expect(numberInputs.length).toBeGreaterThan(0);
		await user.clear(numberInputs[0]);
		await user.type(numberInputs[0], "0.7");

		expect(onParamsChange).toHaveBeenCalled();
	});

	it("shows ApplyRecommendedButton when settings are opened", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		// Open settings panel first
		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// Apply Recommended button exists (shows "Loading..." initially)
		const applyButton = screen.getByRole("button", { name: /Loading/i });
		expect(applyButton).toBeInTheDocument();
		expect(applyButton).toBeDisabled();
	});

	it("applies accent tint class when tint is accent", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="accent" />,
		);

		expect(container.firstChild).toHaveClass("ui-card-tint-accent");
	});

	it("applies blue tint class when tint is blue", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="blue" />,
		);

		expect(container.firstChild).toHaveClass("ui-card-tint-blue");
	});

	it("does not apply tint class when tint is default", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="default" />,
		);

		expect(container.firstChild).not.toHaveClass("ui-card-tint-accent");
		expect(container.firstChild).not.toHaveClass("ui-card-tint-blue");
	});

	it("applies pulse border class when pulseBorder is true", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} pulseBorder />,
		);

		expect(container.firstChild).toHaveClass(
			"animate-[pulse-border_2s_ease-in-out_infinite]",
		);
	});

	it("does not apply pulse border class when pulseBorder is false", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} pulseBorder={false} />,
		);

		expect(container.firstChild).not.toHaveClass(
			"animate-[pulse-border_2s_ease-in-out_infinite]",
		);
	});

	it("shows close button when onClose is provided", () => {
		const onClose = vi.fn();
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} onClose={onClose} />,
		);

		expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
	});

	it("does not show close button when onClose is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.queryByRole("button", { name: "Close" }),
		).not.toBeInTheDocument();
	});

	it("calls onClose when close button is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} onClose={onClose} />,
		);

		await user.click(screen.getByRole("button", { name: "Close" }));

		expect(onClose).toHaveBeenCalled();
	});

	it("does not apply card wrapper when embedded is true", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} embedded />,
		);

		expect(container.firstChild).not.toHaveClass("ui-card");
	});

	it("applies card wrapper when embedded is false", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} embedded={false} />,
		);

		expect(container.firstChild).toHaveClass("ui-card");
	});

	it("uses display_name as title when available", () => {
		const modelWithDisplayName = {
			...mockModel,
			display_name: "Custom Display Name",
		};

		renderWithProviders(<ModelDetailPanel model={modelWithDisplayName} />);

		expect(screen.getByText("Custom Display Name")).toBeInTheDocument();
	});

	it("uses model_id as title when display_name is not available", () => {
		const modelWithoutDisplayName = {
			...mockModel,
			display_name: "",
		};

		renderWithProviders(<ModelDetailPanel model={modelWithoutDisplayName} />);

		// Title appears in h3 element
		const title = screen.getByRole("heading", { level: 3 });
		expect(title).toHaveTextContent("test-model-v1");
	});
});

describe("ModelDetailModal", () => {
	const onClose = vi.fn();

	it("renders Modal wrapper with ModelDetailPanel inside", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		expect(screen.getByText("Test Model v1")).toBeInTheDocument();
		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("passes collapsible prop to ModelDetailPanel", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} collapsible />,
		);

		expect(
			screen.getByRole("button", { name: /Collapse model details/i }),
		).toBeInTheDocument();
	});

	it("does not show collapsible toggle by default", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		expect(
			screen.queryByRole("button", { name: /Collapse model details/i }),
		).not.toBeInTheDocument();
	});

	it("passes embedded prop to ModelDetailPanel", () => {
		const { container } = renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		// The panel should not have the outer card wrapper when embedded
		const panel = container.querySelector(".text-xs.relative.overflow-y-auto");
		expect(panel).toBeInTheDocument();
	});

	it("calls onClose when modal is closed", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(onClose).toHaveBeenCalled();
	});
});
