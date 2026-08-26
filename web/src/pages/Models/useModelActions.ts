import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";

export interface ModelTestResult {
	success: boolean;
	streaming: boolean;
	ttft_ms: number;
	duration_ms: number;
	response: string;
	error?: string;
}

/**
 * The modal's two provider-facing actions and their transient state: a
 * re-discovery with a 30s cooldown, and a test request whose outcome toasts
 * and flashes the button red for three seconds on failure. Every timer is
 * cleared on unmount.
 */
export function useModelActions({
	model,
	onDiscover,
	onTest,
	onToast,
}: {
	model: Model;
	onDiscover?: (providerId: string) => Promise<unknown>;
	onTest?: (id: string) => Promise<ModelTestResult>;
	onToast?: (msg: string, type?: "success" | "error" | "info") => void;
}) {
	const { t } = useTranslation();
	const [cooldown, setCooldown] = useState(0);
	const [discovering, setDiscovering] = useState(false);
	const [testing, setTesting] = useState(false);
	const [testError, setTestError] = useState(false);
	const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
	const testErrorTimers = useRef<ReturnType<typeof setTimeout>[]>([]);

	useEffect(() => {
		return () => {
			if (timerRef.current) clearInterval(timerRef.current);
			// eslint-disable-next-line react-hooks/exhaustive-deps -- cleanup reads ref at unmount time
			for (const timer of testErrorTimers.current) clearTimeout(timer);
		};
	}, []);

	const handleDiscover = async () => {
		if (!onDiscover || cooldown > 0 || discovering) return;
		setDiscovering(true);
		try {
			await onDiscover(model.provider_id);
			setCooldown(30);
			timerRef.current = setInterval(() => {
				setCooldown((prev) => {
					if (prev <= 1) {
						if (timerRef.current) clearInterval(timerRef.current);
						return 0;
					}
					return prev - 1;
				});
			}, 1000);
		} finally {
			setDiscovering(false);
		}
	};

	const flashTestError = () => {
		setTestError(true);
		const timer = setTimeout(() => setTestError(false), 3000);
		testErrorTimers.current.push(timer);
	};

	const handleTest = async () => {
		if (!onTest || !onToast || testing) return;
		setTesting(true);
		setTestError(false);
		try {
			const result = await onTest(model.id);
			if (result.success) {
				const content = result.response.replace(/\n/g, " ").slice(0, 80);
				const isStreaming = result.streaming;
				const ttftPart = isStreaming
					? ` | TTFT: ${(result.ttft_ms / 1000).toFixed(1)}s`
					: "";
				onToast(
					t(
						// Reasoning models can succeed with empty content (the budget
						// went to reasoning); omit the "Response:" part in that case.
						content
							? "models.detail.testSuccess"
							: "models.detail.testSuccessNoResponse",
						{
							content,
							ttftPart,
							duration: (result.duration_ms / 1000).toFixed(1),
						},
					),
					"success",
				);
			} else {
				flashTestError();
				onToast(
					t("models.detail.testFailed", {
						error: result.error || t("common.unknownError"),
					}),
					"error",
				);
			}
		} catch (err) {
			flashTestError();
			onToast(
				t("models.detail.testFailed", {
					error: err instanceof Error ? err.message : t("common.unknownError"),
				}),
				"error",
			);
		} finally {
			setTesting(false);
		}
	};

	return {
		cooldown,
		discovering,
		testing,
		testError,
		handleDiscover,
		handleTest,
	};
}
