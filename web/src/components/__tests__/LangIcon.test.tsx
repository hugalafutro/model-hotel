import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { LangIcon } from "../langIcons";

const mocks = vi.hoisted(() => ({
	useTheme: vi.fn(),
}));

vi.mock("../../context/ThemeContext", async (importOriginal) => {
	const actual = await importOriginal();
	return {
		...(actual as Record<string, unknown>),
		useTheme: mocks.useTheme,
	};
});

describe("LangIcon", () => {
	beforeEach(() => {
		mocks.useTheme.mockReturnValue({
			theme: "dark",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
	});

	it("renders curl icon as SVG", () => {
		renderWithProviders(<LangIcon name="curl" />);
		const svg = screen.getByTitle("cURL");
		expect(svg).toBeInTheDocument();
	});

	it("renders claude icon as img with default size 14", () => {
		renderWithProviders(<LangIcon name="claude" />);
		const img = screen.getByAltText("Claude Code");
		expect(img).toBeInTheDocument();
		expect(img).toHaveAttribute("width", "14");
	});

	it("renders python icon as SVG", () => {
		renderWithProviders(<LangIcon name="python" />);
		const svg = screen.getByTitle("Python");
		expect(svg).toBeInTheDocument();
	});

	it("renders hermes icon with dark theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "dark",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="hermes" />);
		const img = screen.getByAltText("Hermes");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("hermes-dark");
	});

	it("renders hermes icon with light theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "light",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="hermes" />);
		const img = screen.getByAltText("Hermes");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("hermes-light");
	});

	it("renders zed icon with dark theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "dark",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="zed" />);
		const img = screen.getByAltText("ZED");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("zed-dark");
	});

	it("renders zed icon with light theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "light",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="zed" />);
		const img = screen.getByAltText("ZED");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("zed-light");
	});

	it("renders opencode icon with dark theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "dark",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="opencode" />);
		const img = screen.getByAltText("OpenCode");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("opencode-logo-dark");
	});

	it("renders opencode icon with light theme src", () => {
		mocks.useTheme.mockReturnValue({
			theme: "light",
			setTheme: vi.fn(),
			uiStyle: "clean-saas",
			setUIStyle: vi.fn(),
			accentColor: "#546de5",
			setAccentColor: vi.fn(),
			accentPresets: [],
		});
		renderWithProviders(<LangIcon name="opencode" />);
		const img = screen.getByAltText("OpenCode");
		expect(img).toBeInTheDocument();
		expect(img.getAttribute("src")).toContain("opencode-logo-light");
	});

	it("renders librechat icon as img", () => {
		renderWithProviders(<LangIcon name="librechat" />);
		const img = screen.getByAltText("LibreChat");
		expect(img).toBeInTheDocument();
	});

	it("renders openclaw icon as img", () => {
		renderWithProviders(<LangIcon name="openclaw" />);
		const img = screen.getByAltText("OpenClaw");
		expect(img).toBeInTheDocument();
	});

	it("renders javascript icon as SVG", () => {
		renderWithProviders(<LangIcon name="javascript" />);
		const svg = screen.getByTitle("JavaScript");
		expect(svg).toBeInTheDocument();
	});

	it("renders powershell icon as image", () => {
		renderWithProviders(<LangIcon name="powershell" />);
		const img = screen.getByAltText("PowerShell");
		expect(img).toBeInTheDocument();
	});

	it("respects custom size prop", () => {
		renderWithProviders(<LangIcon name="claude" size={24} />);
		const img = screen.getByAltText("Claude Code");
		expect(img).toHaveAttribute("width", "24");
		expect(img).toHaveAttribute("height", "24");
	});
});
