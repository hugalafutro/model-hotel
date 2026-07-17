import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { OutputBadges } from "../OutputBadges";

describe("OutputBadges", () => {
	it("renders a pill per non-text output modality", () => {
		render(<OutputBadges outputModalities='["text","image"]' />);
		expect(screen.getByText("Image out")).toBeInTheDocument();
	});

	it("renders embedding and rerank pills", () => {
		render(<OutputBadges outputModalities='["embedding"]' />);
		expect(screen.getByText("Embedding")).toBeInTheDocument();
		render(<OutputBadges outputModalities='["rerank"]' />);
		expect(screen.getByText("Rerank")).toBeInTheDocument();
	});

	it("renders audio and video generation pills", () => {
		render(<OutputBadges outputModalities='["audio"]' />);
		expect(screen.getByText("Audio out")).toBeInTheDocument();
		render(<OutputBadges outputModalities='["video"]' />);
		expect(screen.getByText("Video out")).toBeInTheDocument();
	});

	it("renders nothing for text-only, empty, or malformed outputs", () => {
		for (const raw of ['["text"]', "", "not-json", "[]"]) {
			const { container } = render(<OutputBadges outputModalities={raw} />);
			expect(container.firstChild).toBeNull();
		}
	});

	it("ignores unknown output modalities", () => {
		const { container } = render(
			<OutputBadges outputModalities='["hologram"]' />,
		);
		expect(container.firstChild).toBeNull();
	});

	it("uses the ui-badge class", () => {
		render(<OutputBadges outputModalities='["image"]' />);
		expect(screen.getByText("Image out")).toHaveClass("ui-badge");
	});
});
