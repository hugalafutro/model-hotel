import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { RequestLogDetail } from "../RequestLogDetail";

const baseLog: LogEntry = {
	id: "test-id",
	provider_id: "prov-1",
	provider_name: "test-provider",
	model_id: "model-1",
	request_hash: "hash123",
	status_code: 200,
	latency_ms: 500,
	duration_ms: 500,
	ttft_ms: 0,
	response_header_ms: 50,
	proxy_overhead_ms: 10,
	parse_ms: 1.0,
	failover_lookup_ms: 0.5,
	model_lookup_ms: 0.5,
	provider_lookup_ms: 0.5,
	key_decrypt_ms: 0.5,
	dial_ms: 10.0,
	settings_read_ms: 0.5,
	cache_hits: null,
	tokens_per_second: null,
	tokens_prompt: 0,
	tokens_completion: 0,
	tokens_prompt_cache_hit: 0,
	tokens_prompt_cache_miss: 0,
	tokens_completion_reasoning: 0,
	streaming: false,
	state: "completed",
	virtual_key_name: "test-key",
	error_message: "",
	failover_attempt: 0,
	created_at: "2024-01-01T00:00:00Z",
	resolved_model_id: "",
	endpoint_type: "chat",
};

const onClose = () => {};

describe("RequestLogDetail endpoint badge", () => {
	it.each([
		["embeddings", "Embeddings"],
		["image", "Image"],
		["tts", "TTS"],
		["stt", "STT"],
	])("shows the %s badge in the header", async (endpointType, label) => {
		const log: LogEntry = { ...baseLog, endpoint_type: endpointType };
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			expect(screen.getByTestId("endpoint-type-badge")).toBeInTheDocument();
		});
		expect(screen.getByTestId("endpoint-type-badge")).toHaveTextContent(label);
	});

	it("shows no badge for chat requests", async () => {
		renderWithProviders(
			<RequestLogDetail requestLog={baseLog} onClose={onClose} />,
		);

		await waitFor(() => {
			expect(screen.getByText("test-provider")).toBeInTheDocument();
		});
		expect(screen.queryByTestId("endpoint-type-badge")).not.toBeInTheDocument();
	});

	it("shows no badge when endpoint_type is absent (legacy rows)", async () => {
		const log = { ...baseLog, endpoint_type: "" };
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			expect(screen.getByText("test-provider")).toBeInTheDocument();
		});
		expect(screen.queryByTestId("endpoint-type-badge")).not.toBeInTheDocument();
	});
});
