import { afterEach, describe, expect, it } from "vitest";
import { applyFavicon, buildFaviconSvg } from "../favicon";

describe("buildFaviconSvg", () => {
	it("tints every shape with the supplied accent color", () => {
		const svg = buildFaviconSvg("#e0823f", "clean-saas");
		expect(svg).toContain('fill="#e0823f"');
		// Three window rows + roof + base all carry the accent.
		expect(svg.match(/fill="#e0823f"/g)?.length).toBe(5);
	});

	it("rounds corners for the SaaS theme and squares them for the terminal theme", () => {
		expect(buildFaviconSvg("#e0823f", "clean-saas")).toContain('rx="10"');
		const terminal = buildFaviconSvg("#2ed573", "cyber-terminal");
		expect(terminal).toContain('rx="0"');
		expect(terminal).not.toContain('rx="10"');
	});

	it("produces valid standalone SVG markup", () => {
		const svg = buildFaviconSvg("#35cfc3", "glassmorphism-lite");
		expect(svg.startsWith("<svg")).toBe(true);
		expect(svg.trimEnd().endsWith("</svg>")).toBe(true);
	});
});

describe("applyFavicon", () => {
	afterEach(() => {
		for (const l of document.querySelectorAll("link[rel='icon']")) {
			l.remove();
		}
	});

	it("updates the existing favicon link with an accent-tinted data URI", () => {
		const link = document.createElement("link");
		link.rel = "icon";
		document.head.appendChild(link);

		applyFavicon("#e0823f", "clean-saas");

		expect(link.href.startsWith("data:image/svg+xml,")).toBe(true);
		const decoded = decodeURIComponent(
			link.href.replace("data:image/svg+xml,", ""),
		);
		expect(decoded).toContain('fill="#e0823f"');
	});

	it("is a no-op when no favicon link is present", () => {
		expect(() => applyFavicon("#e0823f", "clean-saas")).not.toThrow();
	});
});
