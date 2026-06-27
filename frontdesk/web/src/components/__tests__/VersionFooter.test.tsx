import { render, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { expect, it } from "vitest";
import { server } from "../../test/server";
import { VersionFooter } from "../VersionFooter";

const REPO = "https://github.com/hugalafutro/model-hotel";

// The footer renders two links: the build link (repo or a commit under it) and
// the HA wiki link (/wiki/...). Pick the build link by excluding the wiki one.
function buildLink(): HTMLAnchorElement {
	const link = (screen.getAllByRole("link") as HTMLAnchorElement[]).find(
		(l) => l.href.startsWith(REPO) && !l.href.includes("/wiki/"),
	);
	if (!link) throw new Error("build link not found");
	return link;
}

it("dev build with a commit: shows dev + sha and deep-links to the commit", async () => {
	server.use(
		http.get("/api/version", () =>
			HttpResponse.json({ app_version: "dev", app_commit: "abc123def456" }),
		),
	);
	render(<VersionFooter />);

	const link = await screen.findByRole("link", { name: /abc123def456/ });
	expect(link).toHaveAttribute("href", `${REPO}/commit/abc123def456`);
	expect(link.textContent).toContain("dev");

	// The HA wiki link is always present alongside the build link.
	const wiki = screen.getByRole("link", { name: /HA Wiki/ });
	expect(wiki.getAttribute("href")).toContain("/wiki/");
});

it("release build: shows the tag and links to the repo (no commit deep-link)", async () => {
	server.use(
		http.get("/api/version", () =>
			HttpResponse.json({ app_version: "v1.2.3", app_commit: "abc123def456" }),
		),
	);
	render(<VersionFooter />);

	const link = await screen.findByRole("link", { name: /v1\.2\.3/ });
	// A release does not advertise its commit, so the link stays on the repo root
	// and the SHA is not shown.
	expect(link).toHaveAttribute("href", REPO);
	expect(link.textContent).not.toContain("abc123def456");
});

it("git-describe build (tag + commits): treated as dev, shows + deep-links the commit", async () => {
	// `make frontdesk-build` falls back to `git describe` when .version is absent,
	// stamping e.g. "v1.2.3-15-gabc123". That is a dev build, not the v1.2.3
	// release, so the footer must surface the commit and deep-link to it.
	server.use(
		http.get("/api/version", () =>
			HttpResponse.json({
				app_version: "v1.2.3-15-gabc123",
				app_commit: "abc123def456",
			}),
		),
	);
	render(<VersionFooter />);

	const link = await screen.findByRole("link", { name: /abc123def456/ });
	expect(link).toHaveAttribute("href", `${REPO}/commit/abc123def456`);
});

it("dev build without a stamped commit: shows only dev, links to the repo", async () => {
	server.use(
		http.get("/api/version", () =>
			HttpResponse.json({ app_version: "dev", app_commit: "unknown" }),
		),
	);
	render(<VersionFooter />);

	await waitFor(() => expect(buildLink()).toHaveAttribute("href", REPO));
	expect(buildLink().textContent).not.toMatch(/[0-9a-f]{7,}/);
});

it("hides the footer when the version probe fails", async () => {
	server.use(
		http.get("/api/version", () => new HttpResponse(null, { status: 500 })),
	);
	const { container } = render(<VersionFooter />);

	// Give the rejected fetch a tick to settle, then confirm nothing rendered.
	await new Promise((r) => setTimeout(r, 0));
	expect(container).toBeEmptyDOMElement();
});
