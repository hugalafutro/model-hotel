import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "../../api/types";

/**
 * Keeps the message list pinned to the bottom as messages and tokens arrive,
 * unless the user deliberately scrolled up to read earlier messages. A new
 * message resets that intent (the user caused it); streaming respects it.
 */
export function useChatScroll(messages: ChatMessage[], isStreaming: boolean) {
	const messagesContainerRef = useRef<HTMLDivElement>(null);

	const scrollToBottom = useCallback((smooth = false) => {
		requestAnimationFrame(() => {
			const el = messagesContainerRef.current;
			if (!el) return;
			if (smooth) {
				el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
			} else {
				el.scrollTop = el.scrollHeight;
			}
		});
	}, []);

	// Track whether the user deliberately scrolled up to read earlier messages.
	// Reset on new user messages and when scrolling back to bottom.
	const userScrolledUpRef = useRef(false);

	// Attach scroll listener to detect user scrolling up vs programmatic scroll.
	useEffect(() => {
		const el = messagesContainerRef.current;
		if (!el) return;
		const onScroll = () => {
			const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
			userScrolledUpRef.current = !atBottom;
		};
		el.addEventListener("scroll", onScroll, { passive: true });
		return () => el.removeEventListener("scroll", onScroll);
	}, []);

	// Scroll on new messages. Reset userScrolledUp since the user initiated this.
	const messagesLen = messages.length;
	// biome-ignore lint/correctness/useExhaustiveDependencies: scroll on new messages
	useEffect(() => {
		userScrolledUpRef.current = false;
		scrollToBottom(true);
		const timer = setTimeout(() => scrollToBottom(false), 300);
		return () => clearTimeout(timer);
	}, [messagesLen, scrollToBottom]);

	// Smooth auto-scroll during streaming — follows tokens as they arrive.
	const streamingContentLen = messages.reduce(
		(sum, m) => sum + m.content.length,
		0,
	);
	// biome-ignore lint/correctness/useExhaustiveDependencies: streamingContentLen triggers re-scroll on streaming updates
	useEffect(() => {
		if (!isStreaming) return;
		if (userScrolledUpRef.current) return;
		scrollToBottom(true);
	}, [streamingContentLen, isStreaming, scrollToBottom]);

	return { messagesContainerRef, scrollToBottom };
}
