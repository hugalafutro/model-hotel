import { Bell } from "lucide-react";
import type React from "react";
import { useTranslation } from "react-i18next";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";

interface ToastSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function ToastSettings({ collapsed, onToggle }: ToastSettingsProps) {
	const { t } = useTranslation();
	const {
		toast,
		position: toastPosition,
		setPosition,
		timeout,
		setTimeout: setToastTimeout,
	} = useToast();

	return (
		<SettingsSection
			icon={Bell}
			title={t("settings.toast.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<p className="text-gray-400 text-sm mb-4">
				{t("settings.toast.description")}
			</p>
			<div className="flex justify-center">
				<div className="relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50 overflow-visible">
					{/* top-left */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-left");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute top-2 left-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-left"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.topLeft")}
					/>
					{/* top-center */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-center");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute top-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-center"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.topCenter")}
					/>
					{/* top-right */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-right");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute top-2 right-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-right"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.topRight")}
					/>
					{/* bottom-left */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-left");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute bottom-2 left-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-left"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.bottomLeft")}
					/>
					{/* bottom-center */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-center");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute bottom-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-center"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.bottomCenter")}
					/>
					{/* bottom-right */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-right");
							toast(t("settings.toast.testNotification"), "info");
						}}
						className={`absolute bottom-2 right-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-right"
								? "bg-(--accent) opacity-100"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title={t("settings.toast.position.bottomRight")}
					/>
				</div>
			</div>

			<p className="text-center text-gray-500 text-xs mt-4 capitalize">
				{toastPosition.replace("-", " ")}
			</p>

			{/* Toast Timeout */}
			<div className="mt-6">
				<div className="flex items-center justify-between mb-3">
					<span className="text-sm text-gray-300 font-medium">
						{t("settings.toast.autoDismiss")}
					</span>
					<span className="text-sm text-gray-400 tabular-nums">
						{t("settings.toast.seconds", {
							seconds: (timeout / 1000).toFixed(1),
						})}
					</span>
				</div>
				<input
					type="range"
					min={1000}
					max={15000}
					step={500}
					value={timeout}
					onChange={(e) => {
						setToastTimeout(Number(e.target.value));
					}}
					style={
						{
							"--slider-fill": `${((timeout - 1000) / (15000 - 1000)) * 100}%`,
						} as React.CSSProperties
					}
					className="toast-timeout-slider"
				/>
				<div className="flex justify-between text-xs text-gray-600 mt-1.5">
					<span>1s</span>
					<span>15s</span>
				</div>
			</div>
		</SettingsSection>
	);
}
