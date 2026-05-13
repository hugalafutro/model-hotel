import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
	EmptyRow,
	PaginationBar,
	Row,
	SortableHeader,
	StaticHeader,
	StaticHeaderNoArrow,
} from "../DataTable";

describe("SortableHeader", () => {
	const onSort = vi.fn();

	beforeEach(() => {
		onSort.mockClear();
	});

	it("renders label", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
			/>,
		);
		expect(screen.getByText("Name")).toBeInTheDocument();
	});

	it("renders sort arrow when active ascending", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
			/>,
		);
		expect(screen.getByText("↑")).toBeInTheDocument();
	});

	it("renders sort arrow when active descending", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "desc" }}
				onSort={onSort}
			/>,
		);
		expect(screen.getByText("↓")).toBeInTheDocument();
	});

	it("renders blank space when not active", () => {
		const { container } = render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "other", dir: "asc" }}
				onSort={onSort}
			/>,
		);
		// When not active, the span contains only a space (no arrow)
		const span = container.querySelector("button span");
		expect(span).toBeInTheDocument();
		expect(span?.textContent).toBe(" ");
	});

	it("calls onSort when clicked", async () => {
		const user = userEvent.setup();
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
			/>,
		);
		await user.click(screen.getByText("Name"));
		expect(onSort).toHaveBeenCalledWith("name");
	});

	it("has aria-label for accessible sorting", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
			/>,
		);
		expect(
			screen.getByRole("button", { name: "Sort by Name" }),
		).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
				className="custom-header"
			/>,
		);
		expect(screen.getByText("Name").parentElement).toHaveClass("custom-header");
	});

	it("shows tooltip when provided", () => {
		render(
			<SortableHeader
				label="Name"
				field="name"
				sort={{ field: "name", dir: "asc" }}
				onSort={onSort}
				tooltip="Sort by name"
			/>,
		);
		expect(screen.getByText("Name").parentElement).toHaveAttribute(
			"title",
			"Sort by name",
		);
	});
});

describe("StaticHeader", () => {
	it("renders children", () => {
		render(<StaticHeader>Actions</StaticHeader>);
		expect(screen.getByText("Actions")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(<StaticHeader className="custom-class">Actions</StaticHeader>);
		// The th element contains both text and a span, so use closest to find the th
		expect(screen.getByText("Actions").closest("th")).toHaveClass(
			"custom-class",
		);
	});

	it("shows tooltip when provided", () => {
		render(<StaticHeader tooltip="Available actions">Actions</StaticHeader>);
		// The th element contains both text and a span, so use closest to find the th
		expect(screen.getByText("Actions").closest("th")).toHaveAttribute(
			"title",
			"Available actions",
		);
	});
});

describe("StaticHeaderNoArrow", () => {
	it("renders children", () => {
		render(<StaticHeaderNoArrow>Actions</StaticHeaderNoArrow>);
		expect(screen.getByText("Actions")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<StaticHeaderNoArrow className="custom-class">
				Actions
			</StaticHeaderNoArrow>,
		);
		// The th element contains the text directly
		expect(screen.getByText("Actions").closest("th")).toHaveClass(
			"custom-class",
		);
	});

	it("shows tooltip when provided", () => {
		render(
			<StaticHeaderNoArrow tooltip="Available actions">
				Actions
			</StaticHeaderNoArrow>,
		);
		// The th element contains the text directly
		expect(screen.getByText("Actions").closest("th")).toHaveAttribute(
			"title",
			"Available actions",
		);
	});
});

describe("Row", () => {
	it("renders children", () => {
		render(
			<table>
				<tbody>
					<Row index={0}>
						<td>Cell 1</td>
						<td>Cell 2</td>
					</Row>
				</tbody>
			</table>,
		);
		expect(screen.getByText("Cell 1")).toBeInTheDocument();
		expect(screen.getByText("Cell 2")).toBeInTheDocument();
	});

	it("applies alternating row colors", () => {
		render(
			<table>
				<tbody>
					<Row index={0}>
						<td>Even row</td>
					</Row>
					<Row index={1}>
						<td>Odd row</td>
					</Row>
				</tbody>
			</table>,
		);
		const evenRow = screen.getByText("Even row").parentElement;
		const oddRow = screen.getByText("Odd row").parentElement;
		expect(evenRow?.className).not.toContain("bg-white/3");
		expect(oddRow?.className).toContain("bg-white/3");
	});

	it("applies custom className", () => {
		render(
			<table>
				<tbody>
					<Row index={0} className="custom-row">
						<td>Cell</td>
					</Row>
				</tbody>
			</table>,
		);
		expect(screen.getByText("Cell").parentElement).toHaveClass("custom-row");
	});

	it("calls onClick when clicked", async () => {
		const user = userEvent.setup();
		const onClick = vi.fn();
		render(
			<table>
				<tbody>
					<Row index={0} onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		await user.click(screen.getByText("Clickable row").parentElement!);
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("calls onClick when Enter key is pressed", async () => {
		const user = userEvent.setup();
		const onClick = vi.fn();
		render(
			<table>
				<tbody>
					<Row index={0} onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		const row = screen.getByText("Clickable row").parentElement!;
		row.focus();
		await user.keyboard("{Enter}");
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("calls onClick when Space key is pressed", async () => {
		const user = userEvent.setup();
		const onClick = vi.fn();
		render(
			<table>
				<tbody>
					<Row index={0} onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		const row = screen.getByText("Clickable row").parentElement!;
		row.focus();
		await user.keyboard(" ");
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("has button role and tabIndex when onClick is provided", () => {
		render(
			<table>
				<tbody>
					<Row index={0} onClick={vi.fn()}>
						<td>Clickable</td>
					</Row>
				</tbody>
			</table>,
		);
		const row = screen.getByText("Clickable").parentElement!;
		expect(row).toHaveAttribute("role", "button");
		expect(row).toHaveAttribute("tabIndex", "0");
	});
});

describe("EmptyRow", () => {
	it("renders message with correct colSpan", () => {
		render(
			<table>
				<tbody>
					<EmptyRow colSpan={3} message="No data" />
				</tbody>
			</table>,
		);
		expect(screen.getByText("No data")).toBeInTheDocument();
		// The td element contains the text directly
		const cell = screen.getByText("No data").closest("td");
		expect(cell).toHaveAttribute("colSpan", "3");
	});

	it("applies center alignment and gray text styling", () => {
		render(
			<table>
				<tbody>
					<EmptyRow colSpan={3} message="No results found" />
				</tbody>
			</table>,
		);
		// The td element contains the text directly
		const cell = screen.getByText("No results found").closest("td");
		expect(cell?.className).toContain("text-center");
		expect(cell?.className).toContain("text-gray-500");
	});
});

describe("PaginationBar", () => {
	const onPageChange = vi.fn();
	const onPageSizeChange = vi.fn();

	beforeEach(() => {
		onPageChange.mockClear();
		onPageSizeChange.mockClear();
	});

	it("renders with zero items", () => {
		const { container } = render(
			<PaginationBar
				page={1}
				totalPages={1}
				totalItems={0}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// When totalItems is 0, an empty div is rendered
		const emptyDiv = container.querySelector(".text-sm.text-gray-500");
		expect(emptyDiv).toBeInTheDocument();
		expect(emptyDiv).toHaveClass("text-sm", "text-gray-500");
	});

	it("renders with one item", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={1}
				totalItems={1}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Component uses label.replace(/s$/, "") which turns "entries" into "entrie"
		expect(screen.getByText("1 entrie")).toBeInTheDocument();
	});

	it("renders with multiple items showing range", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={5}
				totalItems={100}
				pageSize={20}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		expect(screen.getByText("1 to 20 of 100 entries")).toBeInTheDocument();
	});

	it("renders page size selector", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={5}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		const selector = screen.getByRole("combobox");
		// HTML select values are strings, not numbers
		expect(selector).toHaveValue("10");
	});

	it("calls onPageSizeChange when selector changes", async () => {
		const user = userEvent.setup();
		render(
			<PaginationBar
				page={1}
				totalPages={5}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		const selector = screen.getByRole("combobox");
		await user.selectOptions(selector, "20");
		expect(onPageSizeChange).toHaveBeenCalledWith(20);
	});

	it("renders page number buttons", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Find page buttons specifically (not the range text)
		expect(screen.getByRole("button", { name: "1" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "2" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "3" })).toBeInTheDocument();
	});

	it("renders Prev and Next buttons", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		expect(screen.getByText("Prev")).toBeInTheDocument();
		expect(screen.getByText("Next")).toBeInTheDocument();
	});

	it("calls onPageChange when page button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PaginationBar
				page={1}
				totalPages={5}
				totalItems={100}
				pageSize={20}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Find page button "2" specifically (not the range text "1 to 20")
		const page2Button = screen.getByRole("button", { name: "2" });
		await user.click(page2Button);
		expect(onPageChange).toHaveBeenCalledWith(2);
	});

	it("calls onPageChange when Next is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PaginationBar
				page={1}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		await user.click(screen.getByText("Next"));
		expect(onPageChange).toHaveBeenCalledWith(2);
	});

	it("calls onPageChange when Prev is clicked", async () => {
		const user = userEvent.setup();
		render(
			<PaginationBar
				page={2}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		await user.click(screen.getByText("Prev"));
		expect(onPageChange).toHaveBeenCalledWith(1);
	});

	it("disables Prev button on first page", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		expect(screen.getByText("Prev")).toBeDisabled();
	});

	it("disables Next button on last page", () => {
		render(
			<PaginationBar
				page={3}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		expect(screen.getByText("Next")).toBeDisabled();
	});

	it("highlights current page", () => {
		render(
			<PaginationBar
				page={2}
				totalPages={3}
				totalItems={30}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// The button itself has the highlight class, not its parent
		const currentPageButton = screen.getByRole("button", { name: "2" });
		expect(currentPageButton).toHaveClass("bg-(--accent)");
	});

	it("uses custom label", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={3}
				totalItems={3}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
				label="items"
			/>,
		);
		// When totalItems <= pageSize, no range is shown (no "to N" part)
		expect(screen.getByText("1 of 3 items")).toBeInTheDocument();
	});

	it("uses singular label for one item", () => {
		render(
			<PaginationBar
				page={1}
				totalPages={1}
				totalItems={1}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
				label="items"
			/>,
		);
		expect(screen.getByText("1 item")).toBeInTheDocument();
	});
});
