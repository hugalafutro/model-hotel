import { useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

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
}

/** Attachment size ceilings, matched to what the chat endpoints accept. */
const MAX_IMAGE_BYTES = 20 * 1024 * 1024;
const MAX_AUDIO_BYTES = 25 * 1024 * 1024;

/** The file as a data URL, the form the content-parts API expects. A read
 * that fails or is aborted rejects, so the caller can say so. */
function readAsDataUrl(file: File): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onload = () => resolve(reader.result as string);
		reader.onerror = () => reject(reader.error);
		reader.onabort = () => reject(reader.error);
		reader.readAsDataURL(file);
	});
}

export function useMultimodalAttachments(
	hasVision: boolean,
	toast: (
		msg: string,
		severity?: "success" | "error" | "info" | "warning",
	) => void,
): UseMultimodalAttachmentsReturn {
	const { t } = useTranslation();
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

	/** Reads the file and hands its data URL to `attach`; a failed read is
	 * reported like the other rejected attachments and attaches nothing. */
	const readThen = useCallback(
		(file: File, attach: (dataUrl: string) => void) => {
			void readAsDataUrl(file).then(attach, () =>
				toast(t("hooks.useMultimodalAttachments.readFailed"), "error"),
			);
		},
		[t, toast],
	);

	const handlePaste = useCallback(
		(e: React.ClipboardEvent<HTMLTextAreaElement>) => {
			const items = e.clipboardData?.items;
			if (!items) return;

			// If clipboard has text content, let normal paste through
			// (e.g. spreadsheet cells that produce both text/plain and image/png)
			if (Array.from(items).some((i) => i.type.startsWith("text/"))) return;

			for (const item of items) {
				if (item.type.startsWith("image/")) {
					if (!hasVision) {
						toast(
							t("hooks.useMultimodalAttachments.noImageSupport"),
							"warning",
						);
						e.preventDefault();
						return;
					}

					const file = item.getAsFile();
					if (!file) continue;

					if (file.size > MAX_IMAGE_BYTES) {
						toast(t("hooks.useMultimodalAttachments.imageTooLarge"), "error");
						e.preventDefault();
						return;
					}

					readThen(file, (dataUrl) => {
						setPendingImage({
							dataUrl,
							name: file.name || "pasted-image",
						});
						setPendingAudio(null);
						toast(t("hooks.useMultimodalAttachments.imagePasted"), "info");
					});
					e.preventDefault();
					return;
				}
			}

			// Allow normal text paste through — no image found
		},
		[hasVision, toast, t, readThen],
	);

	const handleImageSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > MAX_IMAGE_BYTES) {
				toast(t("hooks.useMultimodalAttachments.imageTooLarge"), "error");
				return;
			}
			readThen(file, (dataUrl) => {
				setPendingImage({ dataUrl, name: file.name });
				setPendingAudio(null); // only one attachment at a time
			});
			// Reset so the same file can be re-selected
			e.target.value = "";
		},
		[t, toast, readThen],
	);

	const handleAudioSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > MAX_AUDIO_BYTES) {
				toast(t("hooks.useMultimodalAttachments.audioTooLarge"), "error");
				return;
			}
			// The extension is the format the API wants ("mp3", "wav", …).
			const format = file.name.split(".").pop()?.toLowerCase() || "mp3";
			readThen(file, (dataUrl) => {
				setPendingAudio({ dataUrl, name: file.name, format });
				setPendingImage(null); // only one attachment at a time
			});
			e.target.value = "";
		},
		[t, toast, readThen],
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
	};
}
