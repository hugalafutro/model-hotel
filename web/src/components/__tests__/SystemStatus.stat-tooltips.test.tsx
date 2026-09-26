import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import i18n from "../../i18n";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

// "X | Y" rows read as one value but hold two, so each half has to describe
// itself: hovering "procs" must not explain CPU, and hovering the memory
// limit must not explain the memory in use. Titles are read back through
// i18next by key so the suite stays locale-independent.
const DB = {
	size_mb: 10,
	cache_hit_ratio: 95,
	cache_window_blocks: 50000,
	connections: 3,
	tx_per_sec: 5.5,
};

const APP = {
	uptime_seconds: 100,
	cpu_percent: 12.5,
	procs: 5,
	memory_current_bytes: 100_000_000,
	memory_limit_bytes: 0,
	in_container: false,
	goroutines: 50,
	requests_today: 0,
	heap_alloc_mb: 100,
	net_rx_bytes_sec: 1000,
	net_tx_bytes_sec: 500,
	disk_read_bytes_sec: 200,
	disk_write_bytes_sec: 100,
};

function serveSystem(body: Record<string, unknown>) {
	server.use(http.get("/api/system", () => HttpResponse.json(body)));
	renderWithProviders(
		<Layout>
			<div />
		</Layout>,
	);
}

/**
 * The element a given tooltip is attached to. A title is not an accessible
 * description, so the same text has to reach the figure as its description
 * too, or keyboard and screen-reader users get nothing.
 */
function tipped(key: string, opts?: Record<string, unknown>) {
	const hint = i18n.t(key, opts ?? {}) as string;
	const el = screen.getByTitle(hint);
	expect(el).toHaveAccessibleDescription(hint);
	return el;
}

describe("SystemStatus stat tooltips", () => {
	it("describes the CPU and procs halves separately", async () => {
		serveSystem({ app: APP, docker: { available: false }, db: DB });

		await waitFor(() => {
			expect(screen.getByText("12.5")).toBeInTheDocument();
		});
		expect(tipped("layout.tooltips.cpu")).toHaveTextContent("12.5");
		expect(tipped("layout.tooltips.procs")).toHaveTextContent("5");
	});

	it("describes the memory used and limit halves separately", async () => {
		serveSystem({
			app: {
				...APP,
				in_container: true,
				memory_current_bytes: 256 * 1024 * 1024,
				memory_limit_bytes: 1024 * 1024 * 1024,
			},
			docker: { available: false },
			db: DB,
		});

		await waitFor(() => {
			expect(
				screen.getByTitle(i18n.t("layout.tooltips.memoryUsed")),
			).toBeInTheDocument();
		});
		expect(tipped("layout.tooltips.memoryUsed")).toHaveTextContent("256");
		expect(tipped("layout.tooltips.memoryLimit")).toHaveTextContent("1.0");
	});

	it("names the container count on both halves under Docker", async () => {
		serveSystem({
			app: APP,
			docker: {
				available: true,
				cpu_percent: 25.5,
				procs: 10,
				memory_usage_bytes: 536870912,
				memory_limit_bytes: 1073741824,
				net_rx_bytes_sec: 2000,
				net_tx_bytes_sec: 1000,
				disk_read_bytes_sec: 400,
				disk_write_bytes_sec: 200,
				container_count: 3,
			},
			db: DB,
		});

		await waitFor(() => {
			expect(
				screen.getByTitle(i18n.t("layout.tooltips.aggregateCpu", { count: 3 })),
			).toBeInTheDocument();
		});
		expect(
			tipped("layout.tooltips.aggregateCpu", { count: 3 }),
		).toHaveTextContent("25.5");
		expect(
			tipped("layout.tooltips.aggregateProcs", { count: 3 }),
		).toHaveTextContent("10");
		expect(
			tipped("layout.tooltips.aggregateMemoryUsed", { count: 3 }),
		).toHaveTextContent("512");
		expect(
			tipped("layout.tooltips.aggregateMemoryLimit", { count: 3 }),
		).toHaveTextContent("1.0");
		// Regression pin: the CPU figure names the containers itself, so its
		// row carries no second tooltip saying the same thing.
		const cpuRow = screen.getByText(i18n.t("layout.stats.cpu")).parentElement;
		expect(cpuRow).not.toHaveAttribute("title");
	});

	it("Regression pin: phrases the container count through plural forms", async () => {
		serveSystem({
			app: APP,
			docker: {
				available: true,
				cpu_percent: 25.5,
				procs: 10,
				memory_usage_bytes: 536870912,
				memory_limit_bytes: 1073741824,
				net_rx_bytes_sec: 2000,
				net_tx_bytes_sec: 1000,
				disk_read_bytes_sec: 400,
				disk_write_bytes_sec: 200,
				container_count: 1,
			},
			db: DB,
		});

		await waitFor(() => {
			expect(
				screen.getByTitle(i18n.t("layout.tooltips.aggregateCpu", { count: 1 })),
			).toBeInTheDocument();
		});
		// "1 compose containers" came from a bare {{count}} key; every
		// count-taking key has to be a plural family in the active language.
		for (const key of [
			"layout.tooltips.aggregateCpu",
			"layout.tooltips.aggregateProcs",
			"layout.tooltips.aggregateMemoryUsed",
			"layout.tooltips.aggregateMemoryLimit",
			"layout.stats.aggregateNetwork",
			"layout.stats.aggregateDisk",
			"layout.stats.aggregateMemory",
		]) {
			expect(i18n.exists(`${key}_other`)).toBe(true);
			expect(i18n.exists(key)).toBe(false);
		}
	});

	it("gives the plain rows no tooltip that just repeats their label", async () => {
		serveSystem({ app: APP, docker: { available: false }, db: DB });

		await waitFor(() => {
			expect(
				screen.getByText(i18n.t("layout.stats.uptime")),
			).toBeInTheDocument();
		});
		// Without Docker aggregates there is nothing a row-level tooltip could
		// add over the label already on screen, so these rows carry none.
		for (const key of ["cpu", "network", "disk", "memory"]) {
			const row = screen.getByText(i18n.t(`layout.stats.${key}`)).parentElement;
			expect(row).not.toHaveAttribute("title");
		}
	});

	it("explains the heap reading when no memory limit is set", async () => {
		serveSystem({ app: APP, docker: { available: false }, db: DB });

		await waitFor(() => {
			expect(
				screen.getByTitle(i18n.t("layout.tooltips.memoryHeap")),
			).toBeInTheDocument();
		});
		expect(tipped("layout.tooltips.memoryHeap")).toHaveTextContent("100");
	});
});
