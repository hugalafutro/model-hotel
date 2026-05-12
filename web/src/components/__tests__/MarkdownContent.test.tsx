import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MarkdownContent } from "../MarkdownContent";

describe("MarkdownContent", () => {
	it("renders plain text as paragraph", () => {
		render(<MarkdownContent>Hello world</MarkdownContent>);
		expect(screen.getByText("Hello world")).toBeInTheDocument();
	});

	it("renders bold text", () => {
		render(<MarkdownContent>**bold text**</MarkdownContent>);
		expect(screen.getByText("bold text")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(<MarkdownContent className="custom-class">Content</MarkdownContent>);
		const container = screen.getByText("Content").parentElement;
		expect(container).toHaveClass("custom-class");
	});

	it("renders links", () => {
		render(<MarkdownContent>[Link text](https://example.com)</MarkdownContent>);
		const link = screen.getByRole("link", { name: "Link text" });
		expect(link).toHaveAttribute("href", "https://example.com");
	});

	it("renders code blocks", () => {
		render(<MarkdownContent>`inline code`</MarkdownContent>);
		expect(screen.getByText("inline code")).toBeInTheDocument();
	});

	it("renders headings", () => {
		render(<MarkdownContent># Heading 1</MarkdownContent>);
		expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
			"Heading 1",
		);
	});

	it("renders multiple paragraphs", () => {
		render(
			<MarkdownContent>First paragraph. Second paragraph.</MarkdownContent>,
		);
		// ReactMarkdown may combine paragraphs; check container has content
		const container = screen.getByText(/First paragraph/).parentElement;
		expect(container).toBeInTheDocument();
	});

	it("renders unordered lists", () => {
		render(<MarkdownContent>- Item 1 - Item 2 - Item 3</MarkdownContent>);
		// Check that a ul element is rendered with list items
		const list = screen.getByRole("list");
		expect(list).toBeInTheDocument();
		expect(list).toHaveTextContent(/Item 1/);
	});

	it("renders ordered lists", () => {
		render(<MarkdownContent>1. First 2. Second 3. Third</MarkdownContent>);
		// Check that an ol element is rendered
		const list = screen.getByRole("list");
		expect(list).toBeInTheDocument();
		expect(list).toHaveTextContent(/First/);
	});

	it("applies MARKDOWN_PROSE_CLASSES by default", () => {
		render(<MarkdownContent>Content</MarkdownContent>);
		const container = screen.getByText("Content").parentElement;
		expect(container).toHaveClass("prose");
		expect(container).toHaveClass("prose-invert");
	});
});
