import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ErrorCallout } from "../ErrorCallout";

describe("ErrorCallout", () => {
	it("announces its message and carries the error callout classes", () => {
		render(<ErrorCallout className="mt-2">Key rejected</ErrorCallout>);
		const box = screen.getByRole("alert");
		expect(box).toHaveTextContent("Key rejected");
		expect(box.className).toContain("ui-callout");
		expect(box.className).toContain("ui-callout-error");
		expect(box.className).toContain("mt-2");
	});
});
