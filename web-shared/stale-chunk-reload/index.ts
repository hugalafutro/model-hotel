// A redeploy replaces the hashed page chunks, so a tab opened before it fails
// the next lazy import with a 404. Vite raises vite:preloadError for that; a
// reload fetches the new index.html and its current chunks. The timestamp guard
// stops a reload loop when the chunk is missing for some other reason; without
// storage to hold that guard there is no reload, and the page error boundary
// shows its reload prompt instead.
const KEY = "staleChunkReloadAt";
const MIN_INTERVAL_MS = 10_000;

export function installStaleChunkReload(win: Window = window): void {
	win.addEventListener("vite:preloadError", (event) => {
		try {
			const last = Number(win.sessionStorage.getItem(KEY)) || 0;
			if (Date.now() - last < MIN_INTERVAL_MS) return;
			win.sessionStorage.setItem(KEY, String(Date.now()));
		} catch {
			return;
		}
		event.preventDefault();
		win.location.reload();
	});
}
