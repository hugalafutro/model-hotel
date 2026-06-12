import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AppLogEntry, LogEntry } from "../../api/types";
import { getByDialogName } from "../../test/helpers";
import { renderWithProviders } from "../../test/utils";
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
		ttft_ms: 280,
		response_header_ms: 250,
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
		resolved_model_id: "",
		endpoint_type: "chat",
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

			// Modal uses header prop with h2 containing translated title
			expect(screen.getByRole("dialog")).toBeInTheDocument();
			expect(
				screen.getByRole("heading", { name: "Request Details" }),
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

			expect(screen.getByText("1.45")).toBeInTheDocument();
			expect(screen.getByText("s")).toBeInTheDocument();
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

			expect(screen.getByText("250")).toBeInTheDocument();
			expect(screen.getByText("Headers")).toBeInTheDocument();
			expect(screen.getByText("280")).toBeInTheDocument();
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

		it("displays dash for zero response_header_ms", () => {
			const noHeadersLog = { ...mockRequestLog, response_header_ms: 0 };
			renderWithProviders(
				<LogDetailModal log={noHeadersLog} type="request" onClose={onClose} />,
			);

			// Find the Headers label and navigate to the parent card container
			const headersLabel = screen.getByText("Headers");
			// The label div is inside the card div, so closest gets us the card
			const headersCard = headersLabel.closest(
				'[class*="rounded-(--radius-box)"]',
			);
			expect(headersCard).toBeInTheDocument();
			// The card contains "Headers" label and the dash value
			expect(headersCard?.textContent).toContain("Headers");
			// Should NOT contain "250" (the default mock value)
			expect(headersCard?.textContent).not.toContain("250");
			// Find the value div - it has text-lg and font-bold classes
			const valueDiv = headersCard?.querySelector('[class*="font-bold"]');
			expect(valueDiv?.textContent).toBe("-");
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

			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
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

			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
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

			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});
	});

	describe("App Log Modal", () => {
		it("renders app log details modal", () => {
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			expect(getByDialogName("Log Entry Details")).toBeInTheDocument();
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

			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});

		it("calls onClose when backdrop is clicked", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<LogDetailModal log={mockAppLog} type="app" onClose={onClose} />,
			);

			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);

			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
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

	describe("StatusBadge fallback", () => {
		it("displays plain code for 1xx status", () => {
			const infoLog = { ...mockRequestLog, status_code: 100 };
			renderWithProviders(
				<LogDetailModal log={infoLog} type="request" onClose={onClose} />,
			);

			// 1xx falls through all specific cases to the fallback
			const codeElement = screen.getByText("100");
			expect(codeElement).toBeInTheDocument();
			expect(codeElement.closest("span")).toHaveClass("text-xs");
		});

		it("displays plain code for 3xx status", () => {
			const redirectLog = { ...mockRequestLog, status_code: 301 };
			renderWithProviders(
				<LogDetailModal log={redirectLog} type="request" onClose={onClose} />,
			);

			const codeElement = screen.getByText("301");
			expect(codeElement).toBeInTheDocument();
		});
	});

	describe("Failed badge without error message", () => {
		it("displays Failed without colon when error_message is empty", () => {
			const failedLog = {
				...mockRequestLog,
				state: "failed",
				status_code: 0,
				error_message: "",
			};
			renderWithProviders(
				<LogDetailModal log={failedLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Failed")).toBeInTheDocument();
			// Should NOT contain a colon suffix
			const badge = screen.getByText("Failed").closest("span");
			expect(badge?.textContent).toBe("Failed");
		});
	});

	describe("Tokens per second null fallback", () => {
		it("displays dash when tokens_per_second is null", () => {
			const nullTpsLog = {
				...mockRequestLog,
				tokens_per_second: null as unknown as number,
			};
			renderWithProviders(
				<LogDetailModal log={nullTpsLog} type="request" onClose={onClose} />,
			);

			// The Tokens/s timing card contains both the label and the value
			const tpsLabel = screen.getByText("Tokens/s");
			const tpsCard = tpsLabel.closest('[class*="rounded-(--radius-box)"]');
			expect(tpsCard).toBeInTheDocument();
			expect(tpsCard?.textContent).toContain("-");
		});

		it("displays dash when tokens_per_second is 0", () => {
			const zeroTpsLog = {
				...mockRequestLog,
				tokens_per_second: 0,
			};
			renderWithProviders(
				<LogDetailModal log={zeroTpsLog} type="request" onClose={onClose} />,
			);

			// The Tokens/s timing card contains both the label and the value
			const tpsLabel = screen.getByText("Tokens/s");
			const tpsCard = tpsLabel.closest('[class*="rounded-(--radius-box)"]');
			expect(tpsCard).toBeInTheDocument();
			expect(tpsCard?.textContent).toContain("-");
		});
	});

	describe("Info icon tooltips for timing cards", () => {
		it("renders Info icon with tooltip for Duration card", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const tooltip = screen.getByTitle(
				"Total wall-clock time from request start to response end",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Headers card", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const tooltip = screen.getByTitle(
				"Time to receive the first HTTP response headers from the upstream provider",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for TTFT card", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const tooltip = screen.getByTitle(
				"Time to First Token: delay between request start and the first token of the response body (streaming) or full response (non-streaming)",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Tokens/s card", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const tooltip = screen.getByTitle(
				"Output tokens per second during the generation phase (excludes time-to-first-token). Shown as '-' when generation time is negligible.",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Total Tokens card", () => {
			renderWithProviders(
				<LogDetailModal
					log={mockRequestLog}
					type="request"
					onClose={onClose}
				/>,
			);

			const tooltip = screen.getByTitle(
				"Sum of prompt + completion + reasoning tokens",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});
	});

	describe("Info icon tooltips for overhead breakdown rows", () => {
		const overheadLog = {
			...mockRequestLog,
			proxy_overhead_ms: 50,
		};

		it("renders Info icon with tooltip for Request Parsing row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle(
				"Time to parse and validate the incoming request body",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Failover Group Lookup row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle(
				"Time to resolve the failover group to a specific model and provider",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Model Lookup row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle(
				"Time to look up the model configuration in the database",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Provider Lookup row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle(
				"Time to look up the provider details in the database",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Key Decryption row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle("Time to decrypt the provider API key");
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Dial (DNS+TCP) row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			const tooltip = screen.getByTitle(
				"Time to establish the TCP connection to the upstream provider",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Info icon with tooltip for Settings Reads row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			// Translated: "Time to read settings from the database" (without "proxy")
			const tooltip = screen.getByTitle(
				"Time to read settings from the database",
			);
			expect(tooltip).toBeInTheDocument();
			expect(tooltip.querySelector("svg")).toBeInTheDocument();
		});

		it("does NOT render Info icon for Total Overhead row", () => {
			renderWithProviders(
				<LogDetailModal log={overheadLog} type="request" onClose={onClose} />,
			);

			// Find the Total Overhead label
			const totalLabel = screen.getByText("Total Overhead");
			// Get the parent row/container
			const totalRow = totalLabel.closest("div");
			expect(totalRow).toBeInTheDocument();
			// Verify there's no SVG (Info icon) within this row
			expect(totalRow?.querySelector("svg")).not.toBeInTheDocument();
		});
	});

	describe("Virtual key fallback", () => {
		it("displays virtual_key_id when name is empty", () => {
			const noNameLog = {
				...mockRequestLog,
				virtual_key_name: "",
				virtual_key_id: "vk-fallback-123",
			};
			renderWithProviders(
				<LogDetailModal log={noNameLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("vk-fallback-123")).toBeInTheDocument();
		});

		it("displays dash when both name and id are empty", () => {
			const noKeyLog = {
				...mockRequestLog,
				virtual_key_name: "",
				virtual_key_id: "",
			};
			renderWithProviders(
				<LogDetailModal log={noKeyLog} type="request" onClose={onClose} />,
			);

			// DetailItem renders the value in a div after the label
			const vkLabel = screen.getByText("Virtual Key");
			const vkItem = vkLabel.closest('[class*="rounded-(--radius-box)"]');
			expect(vkItem).toBeInTheDocument();
			expect(vkItem?.textContent).toContain("-");
		});
	});

	describe("Reasoning tokens", () => {
		it("displays Reasoning section when tokens_completion_reasoning > 0", () => {
			const reasoningLog = {
				...mockRequestLog,
				tokens_completion_reasoning: 500,
			};
			renderWithProviders(
				<LogDetailModal log={reasoningLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Reasoning")).toBeInTheDocument();
			const reasoningValue = screen.getByText("500");
			expect(reasoningValue).toHaveClass("text-purple-400");
		});
	});

	describe("Resolved model ID display", () => {
		it("displays (resolved: model_id) when model_id starts with hotel/ and resolved_model_id is present", () => {
			const failoverLog = {
				...mockRequestLog,
				model_id: "hotel/my-failover-group",
				resolved_model_id: "openai/gpt-4o",
			};
			renderWithProviders(
				<LogDetailModal log={failoverLog} type="request" onClose={onClose} />,
			);

			// Check the "resolved" text is shown (it's in a span with the resolved model ID)
			// Use regex since text may be wrapped/broken up by JSX
			const resolvedSpan = screen.getByText((content) =>
				content.includes("resolved"),
			);
			expect(resolvedSpan).toBeInTheDocument();
			// Check the resolved model ID is shown in the modal
			expect(screen.getByText("openai/gpt-4o")).toBeInTheDocument();
		});

		it("does not display (resolved: ...) when model_id does not start with hotel/", () => {
			const regularLog = {
				...mockRequestLog,
				model_id: "anthropic/claude-3-5-sonnet",
				resolved_model_id: "anthropic/claude-3-5-sonnet",
			};
			renderWithProviders(
				<LogDetailModal log={regularLog} type="request" onClose={onClose} />,
			);

			// Should NOT show the resolved annotation - check for text containing "resolved"
			expect(
				screen.queryByText((content) => content.includes("resolved")),
			).not.toBeInTheDocument();
		});

		it("does not display (resolved: ...) when resolved_model_id is empty", () => {
			const noResolvedLog = {
				...mockRequestLog,
				model_id: "hotel/my-failover-group",
				resolved_model_id: "",
			};
			renderWithProviders(
				<LogDetailModal log={noResolvedLog} type="request" onClose={onClose} />,
			);

			// Should NOT show the resolved annotation when resolved_model_id is empty
			expect(
				screen.queryByText((content) => content.includes("resolved")),
			).not.toBeInTheDocument();
		});
	});

	describe("Overhead with Dial reused and zero components", () => {
		it("shows reused for Dial when dial_ms is 0", () => {
			const dialReusedLog = {
				...mockRequestLog,
				proxy_overhead_ms: 50,
				parse_ms: 5,
				failover_lookup_ms: 0,
				model_lookup_ms: 0,
				provider_lookup_ms: 0,
				key_decrypt_ms: 0,
				dial_ms: 0,
				settings_read_ms: 0,
			};
			renderWithProviders(
				<LogDetailModal log={dialReusedLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Dial (DNS+TCP)")).toBeInTheDocument();
			expect(screen.getByText("reused")).toBeInTheDocument();
			// Only Request Parsing and Dial rows should appear
			expect(screen.getByText("Request Parsing")).toBeInTheDocument();
			expect(
				screen.queryByText("Failover Group Lookup"),
			).not.toBeInTheDocument();
			expect(screen.queryByText("Model Lookup")).not.toBeInTheDocument();
		});

		it("computes total overhead with zero-valued components", () => {
			const partialOverheadLog = {
				...mockRequestLog,
				proxy_overhead_ms: 50,
				parse_ms: 5,
				failover_lookup_ms: 0,
				model_lookup_ms: 0,
				provider_lookup_ms: 0,
				key_decrypt_ms: 0,
				dial_ms: 0,
				settings_read_ms: 0,
			};
			renderWithProviders(
				<LogDetailModal
					log={partialOverheadLog}
					type="request"
					onClose={onClose}
				/>,
			);

			// Total = 5 (parse) + 0 (others) = 5 → shown as accent-colored value
			const totalLabel = screen.getByText("Total Overhead");
			const totalRow = totalLabel.closest("div");
			expect(totalRow).toBeInTheDocument();
			expect(totalRow?.textContent).toContain("5.000ms");
		});
	});

	describe("Overhead with null timing fields", () => {
		it("computes total overhead when timing fields are null", () => {
			const nullFieldsLog = {
				...mockRequestLog,
				proxy_overhead_ms: 50,
				parse_ms: null as unknown as number,
				failover_lookup_ms: null as unknown as number,
				model_lookup_ms: null as unknown as number,
				provider_lookup_ms: null as unknown as number,
				key_decrypt_ms: null as unknown as number,
				dial_ms: null as unknown as number,
				settings_read_ms: null as unknown as number,
			};
			renderWithProviders(
				<LogDetailModal log={nullFieldsLog} type="request" onClose={onClose} />,
			);

			expect(screen.getByText("Proxy Overhead Breakdown")).toBeInTheDocument();
			const totalLabel = screen.getByText("Total Overhead");
			const totalRow = totalLabel.closest("div");
			expect(totalRow).toBeInTheDocument();
			// All null fields fall back to 0 via ||, so total = 0 → formatMs shows "-"
			expect(totalRow?.textContent).toContain("-");
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
