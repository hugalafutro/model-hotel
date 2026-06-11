import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ModelCapabilities } from "../../api/types";
import { CapBadge } from "../CapBadge";

describe("CapBadge", () => {
	const mockCapsWithVision: ModelCapabilities = {
		vision: true,
		reasoning: false,
		tool_calling: true,
		structured_output: false,
		pdf_upload: false,
		video_input: false,
		audio_input: false,
		parallel_tool_calls: false,
	};

	const mockCapsWithAll: ModelCapabilities = {
		vision: true,
		reasoning: true,
		tool_calling: true,
		structured_output: true,
		pdf_upload: true,
		video_input: true,
		audio_input: true,
		parallel_tool_calls: true,
	};

	it("renders visible caps", () => {
		render(<CapBadge caps={mockCapsWithVision} capKey="vision" />);
		expect(screen.getByText("Vision")).toBeInTheDocument();
	});

	it("hides when cap not present in capabilities", () => {
		const { container } = render(
			<CapBadge caps={mockCapsWithVision} capKey="reasoning" />,
		);
		expect(container.firstChild).toBeNull();
	});

	it("hides when caps is null", () => {
		const { container } = render(<CapBadge caps={null} capKey="vision" />);
		expect(container.firstChild).toBeNull();
	});

	it("renders with active variant (default)", () => {
		render(
			<CapBadge caps={mockCapsWithVision} capKey="vision" variant="active" />,
		);
		const badge = screen.getByText("Vision");
		expect(badge).toHaveClass("bg-purple-900/40");
		expect(badge).toHaveClass("text-purple-300");
	});

	it("renders with muted variant", () => {
		render(
			<CapBadge caps={mockCapsWithVision} capKey="vision" variant="muted" />,
		);
		const badge = screen.getByText("Vision");
		expect(badge).toHaveClass("bg-purple-900/15");
		expect(badge).toHaveClass("text-purple-500/60");
	});

	it("renders with disabled variant", () => {
		render(
			<CapBadge caps={mockCapsWithVision} capKey="vision" variant="disabled" />,
		);
		const badge = screen.getByText("Vision");
		expect(badge).toHaveClass("bg-gray-800/30");
		expect(badge).toHaveClass("text-gray-600/40");
	});

	it("renders different cap types with correct styles", () => {
		render(<CapBadge caps={mockCapsWithAll} capKey="tool_calling" />);
		const badge = screen.getByText("Tools");
		expect(badge).toHaveClass("bg-cyan-900/40");
		expect(badge).toHaveClass("text-cyan-300");
	});

	it("returns null for unknown cap key", () => {
		const { container } = render(
			<CapBadge caps={mockCapsWithVision} capKey="reasoning" />,
		);
		expect(container.firstChild).toBeNull();
	});

	it("applies ui-badge class for terminal theme styling", () => {
		render(<CapBadge caps={mockCapsWithVision} capKey="vision" />);
		const badge = screen.getByText("Vision");
		expect(badge).toHaveClass("ui-badge");
	});
});
