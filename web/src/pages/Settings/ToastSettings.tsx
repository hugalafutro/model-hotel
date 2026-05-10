import { Bell } from "lucide-react";
import type React from "react";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";

interface ToastSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function ToastSettings({ collapsed, onToggle }: ToastSettingsProps) {
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
			title="Toast Notifications"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<p className="text-gray-400 text-sm mb-4">
				Choose where notification toasts appear and how long they stay visible.
			</p>
			<div className="flex justify-center">
				<div className="relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50 overflow-visible">
					{/* top-left */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-left");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute top-2 left-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-left"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Top Left"
					/>
					{/* top-center */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-center");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute top-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-center"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Top Center"
					/>
					{/* top-right */}
					<button
						type="button"
						onClick={() => {
							setPosition("top-right");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute top-2 right-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "top-right"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Top Right"
					/>
					{/* bottom-left */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-left");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute bottom-2 left-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-left"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Bottom Left"
					/>
					{/* bottom-center */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-center");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute bottom-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-center"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Bottom Center"
					/>
					{/* bottom-right */}
					<button
						type="button"
						onClick={() => {
							setPosition("bottom-right");
							toast("Test notification - you'll see toasts here", "info");
						}}
						className={`absolute bottom-2 right-2 w-3 h-3 rounded-full transition-all ${
							toastPosition === "bottom-right"
								? "bg-(--accent) scale-125 ring-2 ring-white/40"
								: "bg-(--accent) opacity-30 hover:opacity-70"
						}`}
						title="Bottom Right"
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
						Auto-dismiss
					</span>
					<span className="text-sm text-gray-400 tabular-nums">
						{(timeout / 1000).toFixed(1)}s
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
