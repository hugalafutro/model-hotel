import { memo } from "react";
import { useTranslation } from "react-i18next";

// Front Desk wordmark: the app's favicon (the service-bell mark on its dark
// badge, kept in the favicon's own colours so the header logo matches the
// browser-tab icon exactly) followed by a two-tone wordmark. "Front" is
// neutral, "Desk" picks up the accent, mirroring the Model / Hotel split on the
// main dashboard logo.
export const Logo = memo(function Logo({
	className = "",
}: {
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<svg
			xmlns="http://www.w3.org/2000/svg"
			viewBox="0 0 176 48"
			className={className}
			fill="none"
			aria-label={t("app.title")}
		>
			{/* Favicon mark (public/favicon.svg), scaled to the 48-tall canvas and
			    nudged in so it reads as an app icon beside the wordmark. */}
			<g transform="translate(2 4) scale(0.8333)">
				<rect width="48" height="48" rx="10" fill="#0b0c0f" />
				<rect
					x="22.6"
					y="13"
					width="2.8"
					height="7"
					rx="1.4"
					fill="#e0823f"
					opacity="0.9"
				/>
				<circle cx="24" cy="12" r="2.6" fill="#e0823f" />
				<path d="M10 31C10 17 38 17 38 31Z" fill="#e0823f" opacity="0.9" />
				<rect
					x="7"
					y="31"
					width="34"
					height="4"
					rx="2"
					fill="#e0823f"
					opacity="0.7"
				/>
				<rect
					x="13"
					y="37"
					width="22"
					height="2.6"
					rx="1.3"
					fill="#e0823f"
					opacity="0.45"
				/>
			</g>

			{/* Wordmark: Front Desk */}
			<text
				x="54"
				y="32"
				fontFamily="ui-sans-serif, system-ui, -apple-system, sans-serif"
				fontSize="22"
				fontWeight="600"
				letterSpacing="-0.01em"
			>
				<tspan fill="currentColor">Front </tspan>
				<tspan fill="var(--accent)">Desk</tspan>
			</text>
		</svg>
	);
});
