/**
 * Hands the browser a blob to save under `filename`. The anchor has to be in
 * the document for the click to count, so it is added, clicked and removed;
 * the object URL is revoked once the download has taken it.
 */
export function downloadBlob(blob: Blob, filename: string): void {
	const url = URL.createObjectURL(blob);
	const a = document.createElement("a");
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(url);
}
