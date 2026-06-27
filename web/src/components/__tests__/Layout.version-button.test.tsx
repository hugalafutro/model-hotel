import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockSettings, mockVersionLatest } from "../../test/helpers";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

// A run of hex long enough to be a commit SHA. Used to assert the *absence*
// of a build stamp without coupling to translated tooltip copy.
const SHA_RE = /[0-9a-f]{7,}/;

// The amber glow class on the version span signals "update available". Asserting
// on the class (not translated tooltip text) keeps these tests locale-stable.
const GLOW_CLASS = "text-amber-400";

// The footer has two github.com links (a /wiki link and the repo link); match
// the repo href exactly so we don't accidentally grab the wiki link.
const REPO_HREF = "https://github.com/hugalafutro/model-hotel";

function githubLink(): HTMLAnchorElement {
	const link = screen
		.getAllByRole("link")
		.find((l) => l.getAttribute("href") === REPO_HREF) as
		| HTMLAnchorElement
		| undefined;
	if (!link) throw new Error("version button link not found");
	return link;
}

describe("Layout version button", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("dev build: tooltip shows the build commit and the button does not glow", async () => {
		server.use(
			...mockSettings({
				body: { app_version: "dev", app_commit: "abc123def456" },
			}),
		);
		// Even with a newer release available, a dev build must not advertise it.
		server.use(...mockVersionLatest({ body: { tag_name: "v0.9.81" } }));

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);
		await screen.findByText("page content");

		const link = githubLink();
		await waitFor(() => expect(link.title).toContain("abc123def456"));
		expect(within(link).getByText("dev").className).not.toContain(GLOW_CLASS);
	});

	it("dev build without a stamped commit: no SHA in the tooltip, no glow", async () => {
		server.use(
			...mockSettings({ body: { app_version: "dev", app_commit: "unknown" } }),
		);
		server.use(...mockVersionLatest({ body: { tag_name: "v0.9.81" } }));

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);
		await screen.findByText("page content");

		const link = githubLink();
		// "unknown" is dropped, so the tooltip falls back to the running version
		// with no commit stamp appended.
		await waitFor(() => expect(link.title.length).toBeGreaterThan(0));
		expect(link.title).not.toMatch(SHA_RE);
		expect(within(link).getByText("dev").className).not.toContain(GLOW_CLASS);
	});

	it("release build behind the latest tag: glows and names both versions", async () => {
		// .version ships without a leading "v"; it must still count as a release.
		server.use(...mockSettings({ body: { app_version: "0.9.80" } }));
		server.use(...mockVersionLatest({ body: { tag_name: "v0.9.81" } }));

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);
		await screen.findByText("page content");

		const link = githubLink();
		await waitFor(() =>
			expect(within(link).getByText("0.9.80").className).toContain(GLOW_CLASS),
		);
		expect(link.title).toContain("0.9.80");
		expect(link.title).toContain("v0.9.81");
	});

	it("release build on the latest tag: does not glow", async () => {
		server.use(...mockSettings({ body: { app_version: "0.9.80" } }));
		server.use(...mockVersionLatest({ body: { tag_name: "v0.9.80" } }));

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);
		await screen.findByText("page content");

		const link = githubLink();
		await waitFor(() => expect(within(link).getByText("0.9.80")).toBeTruthy());
		expect(within(link).getByText("0.9.80").className).not.toContain(
			GLOW_CLASS,
		);
		expect(link.title).toContain("0.9.80");
	});

	it("release build when the GitHub fetch fails: does not glow", async () => {
		server.use(...mockSettings({ body: { app_version: "0.9.80" } }));
		// latest stays at its "GitHub" sentinel, so no update can be advertised.
		server.use(...mockVersionLatest({ status: 500 }));

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);
		await screen.findByText("page content");

		const link = githubLink();
		await waitFor(() => expect(within(link).getByText("0.9.80")).toBeTruthy());
		expect(within(link).getByText("0.9.80").className).not.toContain(
			GLOW_CLASS,
		);
		expect(link.title).toContain("0.9.80");
	});
});
