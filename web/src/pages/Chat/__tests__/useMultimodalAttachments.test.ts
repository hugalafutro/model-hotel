import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useMultimodalAttachments } from "../useMultimodalAttachments";

// Mock FileReader
const mockFileReaderReadAsDataURL = vi.fn();
const mockFileReaderResult = "";

class MockFileReader {
	onload: (() => void) | null = null;
	result: string = mockFileReaderResult;

	readAsDataURL() {
		mockFileReaderReadAsDataURL();
		// Simulate async load
		setTimeout(() => {
			if (this.onload) {
				this.result = "data:image/png;base64,mockImageData";
				this.onload();
			}
		}, 0);
	}
}

vi.stubGlobal("FileReader", MockFileReader);

describe("useMultimodalAttachments", () => {
	const mockToast = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
		mockFileReaderReadAsDataURL.mockClear();
	});

	it("initializes with null pendingImage and pendingAudio", () => {
		const { result } = renderHook(() =>
			useMultimodalAttachments(false, mockToast),
		);

		expect(result.current.pendingImage).toBeNull();
		expect(result.current.pendingAudio).toBeNull();
	});

	it("returns hasVision value", () => {
		const { result: resultWithVision } = renderHook(() =>
			useMultimodalAttachments(true, mockToast),
		);
		expect(resultWithVision.current.hasVision).toBe(true);

		const { result: resultWithoutVision } = renderHook(() =>
			useMultimodalAttachments(false, mockToast),
		);
		expect(resultWithoutVision.current.hasVision).toBe(false);
	});

	it("returns imageInputRef and audioInputRef", () => {
		const { result } = renderHook(() =>
			useMultimodalAttachments(false, mockToast),
		);

		expect(result.current.imageInputRef).toBeDefined();
		expect(result.current.audioInputRef).toBeDefined();
		expect(result.current.imageInputRef.current).toBeNull();
		expect(result.current.audioInputRef.current).toBeNull();
	});

	describe("handlePaste with image", () => {
		it("sets pendingImage when pasting image with vision enabled", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			const mockImageFile = new File(["image content"], "pasted.png", {
				type: "image/png",
			});
			const mockClipboardData = {
				items: [
					{
						type: "image/png",
						getAsFile: () => mockImageFile,
					},
				],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			// Wait for FileReader to complete
			await waitFor(() => {
				expect(result.current.pendingImage).not.toBeNull();
			});

			expect(result.current.pendingImage?.name).toBe("pasted.png");
			expect(result.current.pendingImage?.dataUrl).toBe(
				"data:image/png;base64,mockImageData",
			);
			expect(mockToast).toHaveBeenCalledWith(
				"Image pasted from clipboard",
				"info",
			);
		});

		it("shows warning toast when pasting image without vision", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockImageFile = new File(["image content"], "pasted.png", {
				type: "image/png",
			});
			const mockClipboardData = {
				items: [
					{
						type: "image/png",
						getAsFile: () => mockImageFile,
					},
				],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			expect(mockToast).toHaveBeenCalledWith(
				"This model does not support image input",
				"warning",
			);
			expect(result.current.pendingImage).toBeNull();
		});

		it("shows error toast when image exceeds 20MB", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			const largeImageFile = new File(
				[new ArrayBuffer(21 * 1024 * 1024)],
				"large.png",
				{ type: "image/png" },
			);
			const mockClipboardData = {
				items: [
					{
						type: "image/png",
						getAsFile: () => largeImageFile,
					},
				],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			expect(mockToast).toHaveBeenCalledWith(
				"Image must be under 20 MB",
				"error",
			);
			expect(result.current.pendingImage).toBeNull();
		});

		it("clears pendingAudio when setting pendingImage", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			// First set an audio file
			act(() => {
				result.current.setPendingAudio({
					dataUrl: "data:audio/mp3;base64,mock",
					name: "audio.mp3",
					format: "mp3",
				});
			});

			expect(result.current.pendingAudio).not.toBeNull();

			// Now paste an image
			const mockImageFile = new File(["image content"], "pasted.png", {
				type: "image/png",
			});
			const mockClipboardData = {
				items: [
					{
						type: "image/png",
						getAsFile: () => mockImageFile,
					},
				],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).toBeNull();
			});
		});

		it("allows normal text paste through when clipboard has text", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			const mockClipboardData = {
				items: [
					{
						type: "text/plain",
						getAsString: (cb: (s: string) => void) => cb("some text"),
					},
					{
						type: "image/png",
						getAsFile: () =>
							new File(["img"], "img.png", { type: "image/png" }),
					},
				],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			// Should not prevent default, should let text paste through
			expect(mockEvent.preventDefault).not.toHaveBeenCalled();
			expect(result.current.pendingImage).toBeNull();
		});

		it("does nothing when clipboard has no items", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			const mockClipboardData = {
				items: [],
			};

			const mockEvent = {
				clipboardData: mockClipboardData,
				preventDefault: vi.fn(),
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			expect(result.current.pendingImage).toBeNull();
			expect(mockEvent.preventDefault).not.toHaveBeenCalled();
		});

		it("does nothing when clipboardData is undefined", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(true, mockToast),
			);

			const mockEvent = {
				clipboardData: undefined,
			} as unknown as React.ClipboardEvent<HTMLTextAreaElement>;

			act(() => {
				result.current.handlePaste(mockEvent);
			});

			expect(result.current.pendingImage).toBeNull();
		});
	});

	describe("handleImageSelect", () => {
		it("sets pendingImage when valid image file selected", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["image content"], "test.png", {
				type: "image/png",
			});
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleImageSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingImage).not.toBeNull();
			});

			expect(result.current.pendingImage?.name).toBe("test.png");
			expect(result.current.pendingImage?.dataUrl).toBe(
				"data:image/png;base64,mockImageData",
			);
		});

		it("shows error toast when image exceeds 20MB", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const largeFile = new File(
				[new ArrayBuffer(21 * 1024 * 1024)],
				"large.png",
				{ type: "image/png" },
			);
			const mockEvent = {
				target: {
					files: [largeFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleImageSelect(mockEvent);
			});

			expect(mockToast).toHaveBeenCalledWith(
				"Image must be under 20 MB",
				"error",
			);
			expect(result.current.pendingImage).toBeNull();
		});

		it("does nothing when no file selected", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockEvent = {
				target: {
					files: [],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleImageSelect(mockEvent);
			});

			expect(result.current.pendingImage).toBeNull();
			expect(mockToast).not.toHaveBeenCalled();
		});

		it("clears pendingAudio when setting pendingImage", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			// Set audio first
			act(() => {
				result.current.setPendingAudio({
					dataUrl: "data:audio/mp3;base64,mock",
					name: "audio.mp3",
					format: "mp3",
				});
			});

			expect(result.current.pendingAudio).not.toBeNull();

			// Select image
			const mockFile = new File(["image"], "test.png", { type: "image/png" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleImageSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).toBeNull();
			});
		});

		it("resets input value to allow re-selecting same file", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["image"], "test.png", { type: "image/png" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "test.png",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleImageSelect(mockEvent);
			});

			// Input value should be reset
			expect(mockEvent.target.value).toBe("");
		});
	});

	describe("handleAudioSelect", () => {
		it("sets pendingAudio with correct format for mp3", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio content"], "test.mp3", {
				type: "audio/mpeg",
			});
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.name).toBe("test.mp3");
			expect(result.current.pendingAudio?.dataUrl).toBe(
				"data:image/png;base64,mockImageData",
			);
			expect(result.current.pendingAudio?.format).toBe("mp3");
		});

		it("detects wav format from file extension", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.wav", { type: "audio/wav" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("wav");
		});

		it("detects ogg format from file extension", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.ogg", { type: "audio/ogg" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("ogg");
		});

		it("detects m4a format from file extension", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.m4a", { type: "audio/mp4" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("m4a");
		});

		it("detects flac format from file extension", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.flac", { type: "audio/flac" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("flac");
		});

		it("detects webm format from file extension", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.webm", { type: "audio/webm" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("webm");
		});

		it("uses extension as fallback format for unknown types", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.xyz", { type: "audio/xyz" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingAudio).not.toBeNull();
			});

			expect(result.current.pendingAudio?.format).toBe("xyz");
		});

		it("shows error toast when audio exceeds 25MB", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const largeFile = new File(
				[new ArrayBuffer(26 * 1024 * 1024)],
				"large.mp3",
				{ type: "audio/mpeg" },
			);
			const mockEvent = {
				target: {
					files: [largeFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			expect(mockToast).toHaveBeenCalledWith(
				"Audio must be under 25 MB",
				"error",
			);
			expect(result.current.pendingAudio).toBeNull();
		});

		it("does nothing when no file selected", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockEvent = {
				target: {
					files: [],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			expect(result.current.pendingAudio).toBeNull();
			expect(mockToast).not.toHaveBeenCalled();
		});

		it("clears pendingImage when setting pendingAudio", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			// Set image first
			act(() => {
				result.current.setPendingImage({
					dataUrl: "data:image/png;base64,mock",
					name: "image.png",
				});
			});

			expect(result.current.pendingImage).not.toBeNull();

			// Select audio
			const mockFile = new File(["audio"], "test.mp3", { type: "audio/mpeg" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			await waitFor(() => {
				expect(result.current.pendingImage).toBeNull();
			});
		});

		it("resets input value to allow re-selecting same file", async () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			const mockFile = new File(["audio"], "test.mp3", { type: "audio/mpeg" });
			const mockEvent = {
				target: {
					files: [mockFile],
					value: "test.mp3",
				},
			} as unknown as React.ChangeEvent<HTMLInputElement>;

			act(() => {
				result.current.handleAudioSelect(mockEvent);
			});

			// Input value should be reset
			expect(mockEvent.target.value).toBe("");
		});
	});

	describe("setPendingImage and setPendingAudio", () => {
		it("can set pendingImage via setter", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			act(() => {
				result.current.setPendingImage({
					dataUrl: "data:image/png;base64,test",
					name: "manual.png",
				});
			});

			expect(result.current.pendingImage).toEqual({
				dataUrl: "data:image/png;base64,test",
				name: "manual.png",
			});
		});

		it("can set pendingAudio via setter", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			act(() => {
				result.current.setPendingAudio({
					dataUrl: "data:audio/mp3;base64,test",
					name: "manual.mp3",
					format: "mp3",
				});
			});

			expect(result.current.pendingAudio).toEqual({
				dataUrl: "data:audio/mp3;base64,test",
				name: "manual.mp3",
				format: "mp3",
			});
		});

		it("can clear pendingImage by setting null", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			act(() => {
				result.current.setPendingImage({
					dataUrl: "data:image/png;base64,test",
					name: "test.png",
				});
			});

			expect(result.current.pendingImage).not.toBeNull();

			act(() => {
				result.current.setPendingImage(null);
			});

			expect(result.current.pendingImage).toBeNull();
		});

		it("can clear pendingAudio by setting null", () => {
			const { result } = renderHook(() =>
				useMultimodalAttachments(false, mockToast),
			);

			act(() => {
				result.current.setPendingAudio({
					dataUrl: "data:audio/mp3;base64,test",
					name: "test.mp3",
					format: "mp3",
				});
			});

			expect(result.current.pendingAudio).not.toBeNull();

			act(() => {
				result.current.setPendingAudio(null);
			});

			expect(result.current.pendingAudio).toBeNull();
		});
	});
});
