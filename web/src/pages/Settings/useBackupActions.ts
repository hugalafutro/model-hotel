import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, getAuthHeaders } from "../../api/client";
import type { BackupClassification } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { useCopyToClipboard } from "../../hooks/useCopyToClipboard";
import { downloadBlob } from "../../utils/download";
import { SETTING_DEFAULTS, settingOr } from "./defaults";

/**
 * Queries, mutations and the clipboard, download and restore state behind
 * DatabaseBackupSettings, returned as one bag. The card keeps the two
 * handlers that sequence a prune preview and a restore against this state.
 */
export function useBackupActions() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const [restoreFile, setRestoreFile] = useState<File | null>(null);
	const [showRestoreModal, setShowRestoreModal] = useState(false);
	const [isRestoring, setIsRestoring] = useState(false);
	const [showEnableConfirm, setShowEnableConfirm] = useState(false);
	const [prunePreview, setPrunePreview] = useState<BackupClassification | null>(
		null,
	);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const pollingRef = useRef(false);

	useEffect(() => {
		return () => {
			pollingRef.current = false;
		};
	}, []);

	const { data: backups, isLoading } = useQuery({
		queryKey: ["backups"],
		queryFn: () => api.backups.list(),
	});

	const { data: settings, isPending: settingsPending } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	// GFS bucket per backup, so each row can carry a Grandfather/Father/Son tag.
	// Sourced from the prune-preview classifier, which groups every backup by age
	// against the configured retention, so the labels track the same rotation the
	// sliders above configure.
	//
	// Its own key, outside the "backups" prefix: the preview is a POST the server
	// treats as a read, and running it on every backups invalidation (create,
	// delete, prune, restore) would re-classify an unchanged list. The key
	// carries what can change the classification: the set of backups on disk and
	// the retention the classifier applies. It waits for the settings so a late
	// settings answer does not cost a second read (isPending, not undefined, so
	// a settings error still lets the buckets load).
	const backupNames = useMemo(
		() => (backups ?? []).map((b) => b.filename).sort(),
		[backups],
	);
	const { data: classification } = useQuery({
		queryKey: [
			"backup-classification",
			backupNames,
			settings?.backup_son_retention,
			settings?.backup_father_retention,
			settings?.backup_grandfather_retention,
		],
		queryFn: () => api.backups.prunePreview(),
		enabled: backupNames.length > 0 && !settingsPending,
		staleTime: Number.POSITIVE_INFINITY,
	});

	const gfsLabel = useMemo(() => {
		const m = new Map<string, "G" | "F" | "S">();
		for (const b of classification?.grandfather ?? []) m.set(b.filename, "G");
		for (const b of classification?.father ?? []) m.set(b.filename, "F");
		for (const b of classification?.son ?? []) m.set(b.filename, "S");
		return m;
	}, [classification]);

	const createMutation = useMutation({
		mutationFn: () => api.backups.create(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
		},
		onError: (err: Error) => {
			toast(
				t("settings.backup.backupFailed", { message: err.message }),
				"error",
			);
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (filename: string) => api.backups.delete(filename),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
			setConfirmDelete(null);
			toast(t("settings.backup.backupDeleted"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.backup.deleteFailed", { message: err.message }),
				"error",
			);
		},
	});

	// Settings for periodic backup
	const settingsUpdateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToSave", { message: err.message }),
				"error",
			);
		},
	});

	const backupEnabled = settings?.backup_enabled === "true";
	// Parse interval: backend stores as Go duration string (e.g. "86400s" or "24h").
	// Display and edit in hours.
	const rawInterval = settingOr(settings, "backup_interval");
	const intervalHours = (() => {
		const hMatch = rawInterval.match(/^(\d+(?:\.\d+)?)h$/);
		if (hMatch) return Number(hMatch[1]);
		const sMatch = rawInterval.match(/^(\d+(?:\.\d+)?)s$/);
		if (sMatch) return Math.round((Number(sMatch[1]) / 3600) * 10) / 10;
		return Number.parseFloat(SETTING_DEFAULTS.backup_interval);
	})();
	const sonRetention = Number(settingOr(settings, "backup_son_retention"));
	const fatherRetention = Number(
		settingOr(settings, "backup_father_retention"),
	);
	const grandfatherRetention = Number(
		settingOr(settings, "backup_grandfather_retention"),
	);

	// One clipboard path for the whole dashboard (it carries the plain-HTTP
	// fallback itself). The button reports through a toast, so the "Copied" flag
	// is not tracked.
	const { copy } = useCopyToClipboard({ trackCopied: false });

	// Puts the backup's signature sidecar on the clipboard for the restore form.
	// The download hands over the dump alone, so without shell access to the
	// backup directory this is the only way to get the value a verified restore
	// needs.
	const copySignature = async (filename: string) => {
		try {
			const { signature } = await api.backups.signature(filename);
			if (!(await copy(signature))) {
				throw new Error(t("common.failedToCopy"));
			}
			toast(t("settings.backup.signatureCopied"), "success");
		} catch (err) {
			toast(
				t("settings.backup.signatureCopyFailed", {
					message: (err as Error).message,
				}),
				"error",
			);
		}
	};

	// After a restore the server restarts, so the dashboard polls its own API
	// until it answers again. api.backups.list() throws on any non-2xx, so a
	// clean return is the server being back.
	const waitForServer = async () => {
		pollingRef.current = true;
		for (let attempt = 0; pollingRef.current && attempt < 60; attempt++) {
			try {
				await api.backups.list();
				queryClient.invalidateQueries({ queryKey: ["backups"] });
				toast(t("settings.backup.serverBackOnline"), "success");
				return;
			} catch {
				// Server not up yet.
			}
			await new Promise((r) => setTimeout(r, 2000));
		}
		if (pollingRef.current) {
			toast(t("settings.backup.serverRestarting"), "warning");
		}
	};

	const downloadBackup = async (filename: string) => {
		try {
			const response = await fetch(api.backups.downloadUrl(filename), {
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error(`Download failed: ${response.status}`);
			}
			downloadBlob(await response.blob(), filename);
		} catch (err) {
			toast(
				t("settings.backup.downloadFailed", {
					message: (err as Error).message,
				}),
				"error",
			);
		}
	};

	return {
		confirmDelete,
		setConfirmDelete,
		restoreFile,
		setRestoreFile,
		showRestoreModal,
		setShowRestoreModal,
		isRestoring,
		setIsRestoring,
		showEnableConfirm,
		setShowEnableConfirm,
		prunePreview,
		setPrunePreview,
		fileInputRef,
		pollingRef,
		gfsLabel,
		waitForServer,
		createMutation,
		deleteMutation,
		settingsUpdateMutation,
		backupEnabled,
		intervalHours,
		sonRetention,
		fatherRetention,
		grandfatherRetention,
		copySignature,
		downloadBackup,
		backups,
		isLoading,
	};
}

export type BackupActions = ReturnType<typeof useBackupActions>;
