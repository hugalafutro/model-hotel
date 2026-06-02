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
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("Name")).toBeInTheDocument();
	});

	it("renders sort arrow when active ascending", () => {
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("↑")).toBeInTheDocument();
	});

	it("renders sort arrow when active descending", () => {
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "desc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("↓")).toBeInTheDocument();
	});

	it("renders blank space when not active", () => {
		const { container } = render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "other", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		// When not active, the span contains only a space (no arrow)
		const span = container.querySelector("button span");
		expect(span).toBeInTheDocument();
		expect(span?.textContent).toBe(" ");
	});

	it("calls onSort when clicked", async () => {
		const user = userEvent.setup();
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		await user.click(screen.getByText("Name"));
		expect(onSort).toHaveBeenCalledWith("name");
	});

	it("has aria-label for accessible sorting", () => {
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(
			screen.getByRole("button", { name: "Sort by Name" }),
		).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
							className="custom-header"
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("Name").closest("th")).toHaveClass("custom-header");
	});

	it("shows tooltip when provided", () => {
		render(
			<table>
				<thead>
					<tr>
						<SortableHeader
							label="Name"
							field="name"
							sort={{ field: "name", dir: "asc" }}
							onSort={onSort}
							tooltip="Sort by name"
						/>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("Name").closest("th")).toHaveAttribute(
			"title",
			"Sort by name",
		);
	});
});

describe("StaticHeader", () => {
	it("renders children", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeader>Actions</StaticHeader>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("Actions")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeader className="custom-class">Actions</StaticHeader>
					</tr>
				</thead>
			</table>,
		);
		// The th element contains both text and a span, so use closest to find the th
		expect(screen.getByText("Actions").closest("th")).toHaveClass(
			"custom-class",
		);
	});

	it("shows tooltip when provided", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeader tooltip="Available actions">Actions</StaticHeader>
					</tr>
				</thead>
			</table>,
		);
		// The th element contains both text and a span, so use closest to find the th
		expect(screen.getByText("Actions").closest("th")).toHaveAttribute(
			"title",
			"Available actions",
		);
	});
});

describe("StaticHeaderNoArrow", () => {
	it("renders children", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeaderNoArrow>Actions</StaticHeaderNoArrow>
					</tr>
				</thead>
			</table>,
		);
		expect(screen.getByText("Actions")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeaderNoArrow className="custom-class">
							Actions
						</StaticHeaderNoArrow>
					</tr>
				</thead>
			</table>,
		);
		// The th element contains the text directly
		expect(screen.getByText("Actions").closest("th")).toHaveClass(
			"custom-class",
		);
	});

	it("shows tooltip when provided", () => {
		render(
			<table>
				<thead>
					<tr>
						<StaticHeaderNoArrow tooltip="Available actions">
							Actions
						</StaticHeaderNoArrow>
					</tr>
				</thead>
			</table>,
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
					<Row>
						<td>Cell 1</td>
						<td>Cell 2</td>
					</Row>
				</tbody>
			</table>,
		);
		expect(screen.getByText("Cell 1")).toBeInTheDocument();
		expect(screen.getByText("Cell 2")).toBeInTheDocument();
	});

	it("no longer applies bg-white/3 class (striping is CSS nth-child)", () => {
		render(
			<table className="ui-table">
				<tbody>
					<Row>
						<td>Even row</td>
					</Row>
					<Row>
						<td>Odd row</td>
					</Row>
				</tbody>
			</table>,
		);
		const rows = document.querySelectorAll("tbody tr");
		expect(rows[0]).not.toHaveClass("bg-white/3");
		expect(rows[1]).not.toHaveClass("bg-white/3");
	});

	it("applies custom className", () => {
		render(
			<table>
				<tbody>
					<Row className="custom-row">
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
					<Row onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		await user.click(screen.getByText("Clickable row").parentElement!);
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("calls onClick when Enter key is pressed", async () => {
		const user = userEvent.setup();
		const onClick = vi.fn();
		render(
			<table>
				<tbody>
					<Row onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		// biome-ignore lint/style/noNonNullAssertion: test assertion
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
					<Row onClick={onClick}>
						<td>Clickable row</td>
					</Row>
				</tbody>
			</table>,
		);
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		const row = screen.getByText("Clickable row").parentElement!;
		row.focus();
		await user.keyboard(" ");
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("has button role and tabIndex when onClick is provided", () => {
		render(
			<table>
				<tbody>
					<Row onClick={vi.fn()}>
						<td>Clickable</td>
					</Row>
				</tbody>
			</table>,
		);
		// biome-ignore lint/style/noNonNullAssertion: test assertion
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
		// Component uses label.replace(/s$/, "") for singular, then i18n countOne template
		// entries → entrie (artificial singular), countOne = "1 {{label}}" = "1 entrie"
		expect(
			screen.getByText(
				(content) => content.startsWith("1 ") && content.includes("entrie"),
			),
		).toBeInTheDocument();
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
		// Range text is split across multiple nodes, so check the container text
		const countDiv = document.querySelector(".text-sm.text-gray-500");
		expect(countDiv).toBeInTheDocument();
		expect(countDiv?.textContent).toMatch(/1to\s+20\s+of\s+100\s+entries/);
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
		// Text rendered is "1 of 3 items"
		const countDiv = document.querySelector(".text-sm.text-gray-500");
		expect(countDiv).toBeInTheDocument();
		expect(countDiv?.textContent).toMatch(/1\s+of\s+3\s+items/);
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

	it("renders all page numbers when totalPages <= 7", () => {
		render(
			<PaginationBar
				page={4}
				totalPages={7}
				totalItems={70}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Should render buttons 1-7
		for (let i = 1; i <= 7; i++) {
			expect(
				screen.getByRole("button", { name: String(i) }),
			).toBeInTheDocument();
		}
	});

	it("renders first pages and last page when page <= 4 and totalPages > 7", () => {
		render(
			<PaginationBar
				page={3}
				totalPages={10}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Should render: 1, 2, 3, 4, 5, 6, 10 (i=6 → pageNum=totalPages=10)
		for (let i = 1; i <= 6; i++) {
			expect(
				screen.getByRole("button", { name: String(i) }),
			).toBeInTheDocument();
		}
		expect(screen.getByRole("button", { name: "10" })).toBeInTheDocument();
		// Button 7 should NOT exist
		expect(screen.queryByRole("button", { name: "7" })).not.toBeInTheDocument();
	});

	it("renders first page and last pages when page >= totalPages - 3", () => {
		render(
			<PaginationBar
				page={8}
				totalPages={10}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Should render: 1, 5, 6, 7, 8, 9, 10 (i=0 → pageNum=1)
		expect(screen.getByRole("button", { name: "1" })).toBeInTheDocument();
		for (let i = 5; i <= 10; i++) {
			expect(
				screen.getByRole("button", { name: String(i) }),
			).toBeInTheDocument();
		}
		// Button 2 should NOT exist
		expect(screen.queryByRole("button", { name: "2" })).not.toBeInTheDocument();
	});

	it("renders first, middle, and last pages for middle page", () => {
		render(
			<PaginationBar
				page={5}
				totalPages={10}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		// Should render: 1, 3, 4, 5, 6, 7, 10 (else branch: pageNum=page-3+i, with i=0→1, i=6→totalPages)
		expect(screen.getByRole("button", { name: "1" })).toBeInTheDocument();
		for (let i = 3; i <= 7; i++) {
			expect(
				screen.getByRole("button", { name: String(i) }),
			).toBeInTheDocument();
		}
		expect(screen.getByRole("button", { name: "10" })).toBeInTheDocument();
		// Button 2 should NOT exist
		expect(screen.queryByRole("button", { name: "2" })).not.toBeInTheDocument();
	});

	it("clicking a page number calls onPageChange", async () => {
		const user = userEvent.setup();
		render(
			<PaginationBar
				page={1}
				totalPages={10}
				totalItems={100}
				pageSize={10}
				onPageChange={onPageChange}
				onPageSizeChange={onPageSizeChange}
			/>,
		);
		const page3Button = screen.getByRole("button", { name: "3" });
		await user.click(page3Button);
		expect(onPageChange).toHaveBeenCalledWith(3);
	});
});
