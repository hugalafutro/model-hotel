import claudeLogo from "@/assets/logos/claude.png";
import hermesDark from "@/assets/logos/hermes-dark.png";
import hermesLight from "@/assets/logos/hermes-light.png";
import librechatLogo from "@/assets/logos/librechat.png";
import openclawLogo from "@/assets/logos/openclaw.png";
import opencodeDark from "@/assets/logos/opencode-logo-dark.png";
import opencodeLight from "@/assets/logos/opencode-logo-light.png";
import powershellLogo from "@/assets/logos/powershell.png";
import zedDark from "@/assets/logos/zed-dark.png";
import zedLight from "@/assets/logos/zed-light.png";
import { useTheme } from "../context/ThemeContext";

export type LangIconKey =
	| "javascript"
	| "python"
	| "claude"
	| "openclaw"
	| "hermes"
	| "librechat"
	| "curl"
	| "powershell"
	| "zed"
	| "opencode";

interface LangIconProps {
	name: LangIconKey;
	size?: number;
	className?: string;
	style?: React.CSSProperties;
}

/** Small language/tool icons for code example headers. */
export function LangIcon({ name, size = 14, ...rest }: LangIconProps) {
	const { theme } = useTheme();
	const isDark = theme === "dark";

	switch (name) {
		case "curl":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>cURL</title>
					<rect width="24" height="24" rx="4" fill="#111" />
					<path
						d="M5 7l5 5-5 5M13 17h6"
						stroke={isDark ? "#e2e8f0" : "#f1f5f9"}
						strokeWidth={2}
						strokeLinecap="round"
						strokeLinejoin="round"
						fill="none"
					/>
				</svg>
			);

		case "powershell":
			return (
				<img
					src={powershellLogo}
					alt="PowerShell"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "javascript":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>JavaScript</title>
					<rect width="24" height="24" rx="4" fill="#F7DF1E" />
					<path
						d="M6 18.5l2.1-1.3c.4.7.8 1.4 1.6 1.4.8 0 1.3-.3 1.3-1.9V10h2.6v6.8c0 2.7-1.6 3.9-3.8 3.9-2.1 0-3.3-1.1-3.8-2.2zm9-0.2l2.1-1.2c.6 1 1.3 1.7 2.6 1.7 1.1 0 1.8-.5 1.8-1.3 0-.9-.7-1.2-1.9-1.7l-.7-.3c-1.9-.8-3.2-1.8-3.2-3.9 0-2 1.5-3.5 3.9-3.5 1.7 0 2.9.6 3.8 2.1l-2.1 1.3c-.5-.8-1-1.1-1.7-1.1-.8 0-1.3.5-1.3 1.1 0 .8.5 1.1 1.6 1.6l.7.3c2.3 1 3.6 2 3.6 4.2 0 2.4-1.9 3.7-4.4 3.7-2.5 0-4.1-1.2-4.8-2.7"
						fill="#000"
					/>
				</svg>
			);

		case "python":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>Python</title>
					<path
						d="M12 2C7.6 2 7 4 7 4v3h5v1H5.5S2 7.5 2 12s3 5.5 3 5.5H7V14c0-2.2 1.8-4 4-4h4c1.7 0 3-1.3 3-3V4s0-2-6-2zm-2 2a1 1 0 110 2 1 1 0 010-2z"
						fill="#3776AB"
					/>
					<path
						d="M12 22c4.4 0 5-2 5-2v-3h-5v-1h6.5s3.5.5 3.5-4-3-5.5-3-5.5H17V10c0 2.2-1.8 4-4 4h-4c-1.7 0-3 1.3-3 3v4s0 2 6 2zm2-2a1 1 0 110-2 1 1 0 010 2z"
						fill="#FFD43B"
					/>
				</svg>
			);

		case "claude":
			return (
				<img
					src={claudeLogo}
					alt="Claude Code"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "openclaw":
			return (
				<img
					src={openclawLogo}
					alt="OpenClaw"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "hermes":
			return (
				<img
					src={isDark ? hermesDark : hermesLight}
					alt="Hermes"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "librechat":
			return (
				<img
					src={librechatLogo}
					alt="LibreChat"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "zed":
			return (
				<img
					src={isDark ? zedDark : zedLight}
					alt="ZED"
					width={size}
					height={size}
					{...rest}
				/>
			);

		case "opencode":
			return (
				<img
					src={isDark ? opencodeDark : opencodeLight}
					alt="OpenCode"
					width={size}
					height={size}
					{...rest}
				/>
			);
	}
}
