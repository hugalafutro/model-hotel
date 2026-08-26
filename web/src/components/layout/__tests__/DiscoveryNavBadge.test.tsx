import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DiscoveryNavBadge } from "../DiscoveryNavBadge";

const counted = {
	claimCount: 3,
	informationalUnseen: 0,
	hasPinned: false,
	label: "3 claims",
};
const dot = {
	claimCount: 0,
	informationalUnseen: 2,
	hasPinned: false,
	label: "2 unreviewed",
};

describe("DiscoveryNavBadge", () => {
	it("shows the count as a badge and the news as a dot, both named by the label", () => {
		const { rerender } = render(
			<DiscoveryNavBadge badge={counted} onOpen={() => {}} />,
		);
		const badge = screen.getByTestId("discovery-status-badge");
		expect(badge).toHaveAttribute("data-variant", "count");
		expect(badge).toHaveTextContent("3");
		expect(badge).toHaveAccessibleName("3 claims");
		rerender(<DiscoveryNavBadge badge={dot} onOpen={() => {}} />);
		expect(badge).toHaveAttribute("data-variant", "dot");
		expect(badge).toHaveTextContent("");
		expect(badge).toHaveAccessibleName("2 unreviewed");
	});

	it("opens from the keyboard with Enter and Space, since it sits inside a link", async () => {
		const user = userEvent.setup();
		const onOpen = vi.fn();
		render(<DiscoveryNavBadge badge={counted} onOpen={onOpen} />);
		const badge = screen.getByTestId("discovery-status-badge");
		badge.focus();
		await user.keyboard("{Enter}");
		await user.keyboard(" ");
		await user.keyboard("x");
		expect(onOpen).toHaveBeenCalledTimes(2);
		await user.click(badge);
		expect(onOpen).toHaveBeenCalledTimes(3);
	});
});
