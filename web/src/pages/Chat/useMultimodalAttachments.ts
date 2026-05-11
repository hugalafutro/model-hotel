import { useCallback, useRef, useState } from "react";

interface UseMultimodalAttachmentsReturn {
	pendingImage: { dataUrl: string; name: string } | null;
	setPendingImage: React.Dispatch<
		React.SetStateAction<{ dataUrl: string; name: string } | null>
	>;
	pendingAudio: {
		dataUrl: string;
		name: string;
		format: string;
	} | null;
	setPendingAudio: React.Dispatch<
		React.SetStateAction<{
			dataUrl: string;
			name: string;
			format: string;
		} | null>
	>;
	imageInputRef: React.RefObject<HTMLInputElement | null>;
	audioInputRef: React.RefObject<HTMLInputElement | null>;
	handlePaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => void;
	handleImageSelect: (e: React.ChangeEvent<HTMLInputElement>) => void;
	handleAudioSelect: (e: React.ChangeEvent<HTMLInputElement>) => void;
	hasVision: boolean;
}

export function useMultimodalAttachments(
	hasVision: boolean,
	toast: (
		msg: string,
		severity?: "success" | "error" | "info" | "warning",
	) => void,
): UseMultimodalAttachmentsReturn {
	const [pendingImage, setPendingImage] = useState<{
		dataUrl: string;
		name: string;
	} | null>(null);
	const [pendingAudio, setPendingAudio] = useState<{
		dataUrl: string;
		name: string;
		format: string;
	} | null>(null);
	const imageInputRef = useRef<HTMLInputElement>(null);
	const audioInputRef = useRef<HTMLInputElement>(null);

	const handlePaste = useCallback(
		(e: React.ClipboardEvent<HTMLTextAreaElement>) => {
			const items = e.clipboardData?.items;
			if (!items) return;

			// If clipboard has text content, let normal paste through
			// (e.g. spreadsheet cells that produce both text/plain and image/png)
			let hasText = false;
			for (let i = 0; i < items.length; i++) {
				if (items[i].type.startsWith("text/")) {
					hasText = true;
					break;
				}
			}
			if (hasText) return;

			for (const item of items) {
				if (item.type.startsWith("image/")) {
					if (!hasVision) {
						toast("This model does not support image input", "warning");
						e.preventDefault();
						return;
					}

					const file = item.getAsFile();
					if (!file) continue;

					if (file.size > 20 * 1024 * 1024) {
						toast("Image must be under 20 MB", "error");
						e.preventDefault();
						return;
					}

					const reader = new FileReader();
					reader.onload = () => {
						setPendingImage({
							dataUrl: reader.result as string,
							name: file.name || "pasted-image",
						});
						setPendingAudio(null);
						toast("Image pasted from clipboard", "info");
					};
					reader.readAsDataURL(file);
					e.preventDefault();
					return;
				}
			}

			// Allow normal text paste through — no image found
		},
		[hasVision, toast],
	);

	const handleImageSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > 20 * 1024 * 1024) {
				toast("Image must be under 20 MB", "error");
				return;
			}
			const reader = new FileReader();
			reader.onload = () => {
				setPendingImage({ dataUrl: reader.result as string, name: file.name });
				setPendingAudio(null); // only one attachment at a time
			};
			reader.readAsDataURL(file);
			// Reset so the same file can be re-selected
			e.target.value = "";
		},
		[toast],
	);

	const handleAudioSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > 25 * 1024 * 1024) {
				toast("Audio must be under 25 MB", "error");
				return;
			}
			const ext = file.name.split(".").pop()?.toLowerCase() || "mp3";
			const formatMap: Record<string, string> = {
				mp3: "mp3",
				wav: "wav",
				ogg: "ogg",
				m4a: "m4a",
				flac: "flac",
				webm: "webm",
			};
			const format = formatMap[ext] || ext;
			const reader = new FileReader();
			reader.onload = () => {
				setPendingAudio({
					dataUrl: reader.result as string,
					name: file.name,
					format,
				});
				setPendingImage(null); // only one attachment at a time
			};
			reader.readAsDataURL(file);
			e.target.value = "";
		},
		[toast],
	);

	return {
		pendingImage,
		setPendingImage,
		pendingAudio,
		setPendingAudio,
		imageInputRef,
		audioInputRef,
		handlePaste,
		handleImageSelect,
		handleAudioSelect,
		hasVision,
	};
}
