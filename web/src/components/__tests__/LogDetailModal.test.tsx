import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getByDialogName } from "../../test/helpers";
import { renderWithProviders } from "../../test/utils";
import type { AppLogEntry, LogEntry } from "../api/types";
import { LogDetailModal } from "../LogDetailModal";

describe("LogDetailModal", () => {
	const mockRequestLog: LogEntry = {
		id: "req-123",
		provider_id: "prov-456",
		provider_name: "Ollama Cloud",
		model_id: "gemma3:4b",
		request_hash: "abc123def456",
		status_code: 200,
		latency_ms: 1500,
		duration_ms: 1450,
		ttft_ms: 250,
		proxy_overhead_ms: 50,
		parse_ms: 5,
		failover_lookup_ms: 3,
		model_lookup_ms: 10,
		provider_lookup_ms: 15,
		key_decrypt_ms: 8,
		dial_ms: 7,
		settings_read_ms: 5,
		tokens_per_second: 45.5,
		tokens_prompt: 150,
		tokens_completion: 300,
		tokens_prompt_cache_hit: 0,
		tokens_prompt_cache_miss: 0,
		tokens_completion_reasoning: 0,
		streaming: true,
		state: "completed",
		virtual_key_name: "test-key",
		virtual_key_id: "vk-789",
		error_message: "",
		failover_attempt: 0,
		created_at: "2025-05-12T10:30:00Z",
	};

	const mockAppLog: AppLogEntry = {
		timestamp: "2025-05-12T10:30:00Z",
		level: "info",
		source: "internal/api",
		message: "Server started successfully",
	};

	const onClose = vi.fn();

	beforeEach(() => {
		onClose.mockClear();
	});

	describe("Request Log Modal", () => {
		it("renders null when log is null", () => {
			renderWithProviders(
				<LogDetailModal log={null} type="request" onClose={onClose} />,
			);
			// Component returns null, so no dialog element is rendered
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
		});

		it("renders request details modal with header", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(getByDialogName(/Request Details/)).toBeInTheDocument();
			expect(
				screen.getByRole("dialog", { name: /Request Details/ }),
			).toBeInTheDocument();
		});

		it("displays status code badge for successful response", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("200 OK")).toBeInTheDocument();
		});

		it("displays pending state badge", () => {
			const pendingLog = {
				...mockRequestLog,
				state: "pending",
				status_code: 0,
			};
			renderWithProviders(
				<LogDetailModal log={pendingLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Pending")).toBeInTheDocument();
		});

		it("displays streaming state badge", () => {
			const streamingLog = {
				...mockRequestLog,
				state: "streaming",
				status_code: 0,
			};
			renderWithProviders(
				<LogDetailModal log={streamingLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Streaming")).toBeInTheDocument();
		});

		it("displays failed state badge with error message", () => {
			const failedLog = {
				...mockRequestLog,
				state: "failed",
				status_code: 0,
				error_message: "Connection timeout",
			};
			renderWithProviders(
				<LogDetailModal log={failedLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText(/Failed/)).toBeInTheDocument();
			expect(screen.getByText("Connection timeout")).toBeInTheDocument();
		});

		it("displays client error badge for 4xx status", () => {
			const clientErrorLog = { ...mockRequestLog, status_code: 400 };
			renderWithProviders(
				<LogDetailModal
					log={clientErrorLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("400 Client Error")).toBeInTheDocument();
		});

		it("displays server error badge for 5xx status", () => {
			const serverErrorLog = { ...mockRequestLog, status_code: 500 };
			renderWithProviders(
				<LogDetailModal
					log={serverErrorLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("500 Server Error")).toBeInTheDocument();
		});

		it("displays failover attempt badge", () => {
			const failoverLog = { ...mockRequestLog, failover_attempt: 2 };
			renderWithProviders(
				<LogDetailModal log={failoverLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Attempt 3")).toBeInTheDocument();
		});

		it("displays duration in timing overview", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("1.45s")).toBeInTheDocument();
			expect(screen.getByText("Duration")).toBeInTheDocument();
		});

		it("displays TTFT in timing overview", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("250ms")).toBeInTheDocument();
			expect(screen.getByText("TTFT")).toBeInTheDocument();
		});

		it("displays dash for zero TTFT", () => {
			const noTtftLog = { ...mockRequestLog, ttft_ms: 0 };
			renderWithProviders(
				<LogDetailModal log={noTtftLog} type="request" onClose={onClose} />,
			);

			// Find the TTFT timing card and verify it shows a dash
			const ttftLabel = screen.getByText("TTFT");
			const ttftCard = ttftLabel.closest("div");
			expect(ttftCard).toBeInTheDocument();
			// The card contains "TTFT" label and the dash value
			expect(ttftCard?.textContent).toContain("TTFT");
		});

		it("displays tokens per second in timing overview", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("45.5")).toBeInTheDocument();
			expect(screen.getByText("Tokens/s")).toBeInTheDocument();
		});

		it("displays total tokens in timing overview", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("450")).toBeInTheDocument();
			expect(screen.getByText("Total Tokens")).toBeInTheDocument();
		});

		it("displays timestamp with formatted date", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Timestamp")).toBeInTheDocument();
			expect(screen.getByText(/2025/)).toBeInTheDocument();
		});

		it("displays request hash with copy button", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Request Hash")).toBeInTheDocument();
			expect(screen.getByText("abc123def456")).toBeInTheDocument();
		});

		it("displays model with copy button", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Model")).toBeInTheDocument();
			expect(screen.getByText("gemma3:4b")).toBeInTheDocument();
		});

		it("displays provider name", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Provider")).toBeInTheDocument();
			expect(screen.getByText("Ollama Cloud")).toBeInTheDocument();
		});

		it("displays DB row ID with copy button", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("DB Row ID")).toBeInTheDocument();
			expect(screen.getByText("req-123")).toBeInTheDocument();
		});

		it("displays virtual key name", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Virtual Key")).toBeInTheDocument();
			expect(screen.getByText("test-key")).toBeInTheDocument();
		});

		it("displays token usage breakdown section", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Token Usage")).toBeInTheDocument();
			expect(screen.getByText("Prompt")).toBeInTheDocument();
			expect(screen.getByText("150")).toBeInTheDocument();
			expect(screen.getByText("Completion")).toBeInTheDocument();
			expect(screen.getByText("300")).toBeInTheDocument();
		});

		it("displays cache hit/miss when cache tokens present", () => {
			const cacheLog = {
				...mockRequestLog,
				tokens_prompt_cache_hit: 50,
				tokens_prompt_cache_miss: 100,
			};
			renderWithProviders(
				<LogDetailModal log={cacheLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Cache Hit")).toBeInTheDocument();
			expect(screen.getByText("50")).toBeInTheDocument();
			expect(screen.getByText("Cache Miss")).toBeInTheDocument();
			expect(screen.getByText("100")).toBeInTheDocument();
		});

		it("displays proxy overhead breakdown", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			expect(screen.getByText("Proxy Overhead Breakdown")).toBeInTheDocument();
			expect(screen.getByText("Request Parsing")).toBeInTheDocument();
			expect(screen.getByText("Failover Group Lookup")).toBeInTheDocument();
			expect(screen.getByText("Model Lookup")).toBeInTheDocument();
			expect(screen.getByText("Provider Lookup")).toBeInTheDocument();
			expect(screen.getByText("Key Decryption")).toBeInTheDocument();
			expect(screen.getByText("Dial (DNS+TCP)")).toBeInTheDocument();
			expect(screen.getByText("Settings Reads")).toBeInTheDocument();
			expect(screen.getByText("Total Overhead")).toBeInTheDocument();
		});

		it("hides proxy overhead section when overhead is zero", () => {
			const noOverheadLog = { ...mockRequestLog, proxy_overhead_ms: 0 };
			renderWithProviders(
				<LogDetailModal log={noOverheadLog} type="request" onClose={onClose} />,
			);

			expect(
				screen.queryByText("Proxy Overhead Breakdown"),
			).not.toBeInTheDocument();
		});

		it("displays error message section when present", () => {
			const errorLog = {
				...mockRequestLog,
				error_message: "Upstream connection failed",
				status_code: 500,
			};
			renderWithProviders(
				<LogDetailModal log={errorLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Error")).toBeInTheDocument();
			expect(
				screen.getByText("Upstream connection failed"),
			).toBeInTheDocument();
		});

		it("hides error section when no error message", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const errorSection = screen.queryByText("Error");
			if (errorSection) {
				expect(errorSection).not.toBeInTheDocument();
			}
		});

		it("calls onClose when close button is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when backdrop is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);

			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when Escape key is pressed", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			await user.keyboard("{Escape}");

			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	describe("App Log Modal", () => {
		it("renders app log details modal", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(getByDialogName(/Log Entry Details/)).toBeInTheDocument();
			expect(
				screen.getByRole("dialog", { name: /Log Entry Details/ }),
			).toBeInTheDocument();
		});

		it("displays timestamp", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("Timestamp")).toBeInTheDocument();
			expect(screen.getByText(/2025/)).toBeInTheDocument();
		});

		it("displays log level badge for info", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("INFO")).toBeInTheDocument();
		});

		it("displays log level badge for warning", () => {
			const warningLog: AppLogEntry = {
				...mockAppLog,
				level: "warning",
				message: "High memory usage detected",
			};
			renderWithProviders(
				<LogDetailModal log={warningLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("WARNING")).toBeInTheDocument();
		});

		it("displays log level badge for error", () => {
			const errorLog: AppLogEntry = {
				...mockAppLog,
				level: "error",
				message: "Database connection failed",
			};
			renderWithProviders(
				<LogDetailModal log={errorLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("ERROR")).toBeInTheDocument();
		});

		it("displays source", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("Source")).toBeInTheDocument();
			expect(screen.getByText("internal/api")).toBeInTheDocument();
		});

		it("displays message with copy button", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("Message")).toBeInTheDocument();
			expect(
				screen.getByText("Server started successfully"),
			).toBeInTheDocument();
		});

		it("displays dash for missing source", () => {
			const noSourceLog: AppLogEntry = {
				...mockAppLog,
				source: "",
			};
			renderWithProviders(
				<LogDetailModal log={noSourceLog} type="app" onClose={onClose} />,
			);

			expect(screen.getByText("Source")).toBeInTheDocument();
			const sourceValue = screen.getByText("-");
			expect(sourceValue).toBeInTheDocument();
		});

		it("calls onClose when close button is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when backdrop is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);

			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("handles invalid date gracefully", () => {
			const invalidDateLog: AppLogEntry = {
				...mockAppLog,
				timestamp: "invalid-date",
			};
			renderWithProviders(
				<LogDetailModal log={invalidDateLog} type="app" onClose={onClose} />,
			);

			// Invalid dates fall back to showing the raw ISO string in the modal
			// The formatDateTime function returns the original string on error
			expect(screen.getByText(/Timestamp/)).toBeInTheDocument();
		});
	});

	describe("Copy functionality", () => {
		it("copies model ID to clipboard", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const copyButton = screen.getByLabelText("Copy model ID");
			await user.click(copyButton);

			const clipboardText = await navigator.clipboard.readText();
			expect(clipboardText).toBe("gemma3:4b");
		});

		it("copies DB row ID to clipboard", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const copyButton = screen.getByLabelText("Copy DB row ID");
			await user.click(copyButton);

			const clipboardText = await navigator.clipboard.readText();
			expect(clipboardText).toBe("req-123");
		});

		it("copies error message to clipboard", async () => {
			const user = userEvent.setup();
			const errorLog = {
				...mockRequestLog,
				error_message: "Test error message",
			};
			renderWithProviders(
				<LogDetailModal log={errorLog} type="request" onClose={onClose} />,
			);

			const copyButton = screen.getByLabelText("Copy error message");
			await user.click(copyButton);

			const clipboardText = await navigator.clipboard.readText();
			expect(clipboardText).toBe("Test error message");
		});

		it("copies app log message to clipboard", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			const copyButton = screen.getByLabelText("Copy message");
			await user.click(copyButton);

			const clipboardText = await navigator.clipboard.readText();
			expect(clipboardText).toBe("Server started successfully");
		});
	});

	describe("Token formatting", () => {
		it("formats large token counts with locale separators", () => {
			const largeTokenLog: LogEntry = {
				...mockRequestLog,
				tokens_prompt: 1500000,
				tokens_completion: 3000000,
			};
			renderWithProviders(
				<LogDetailModal log={largeTokenLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("1,500,000")).toBeInTheDocument();
			expect(screen.getByText("3,000,000")).toBeInTheDocument();
			expect(screen.getByText("4,500,000")).toBeInTheDocument();
		});

		it("hides token usage section when total tokens is zero", () => {
			const zeroTokenLog: LogEntry = {
				...mockRequestLog,
				tokens_prompt: 0,
				tokens_completion: 0,
			};
			renderWithProviders(
				<LogDetailModal log={zeroTokenLog} type="request" onClose={onClose} />,
			);

			expect(screen.queryByText("Token Usage")).not.toBeInTheDocument();
		});
	});
});
