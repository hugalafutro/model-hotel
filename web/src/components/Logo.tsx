import { memo } from "react";
export const Logo = memo(function Logo({
	className = "",
}: {
	className?: string;
}) {
	return (
		<svg
			xmlns="http://www.w3.org/2000/svg"
			viewBox="0 0 220 48"
			className={className}
			fill="none"
			aria-label="Model Hotel"
		>
			{/* Hotel Building Icon */}
			<g transform="translate(25, 6)">
				{/* Roof */}
				<path
					d="M18 2L4 14v2h28v-2L18 2z"
					fill="currentColor"
					fillOpacity="0.9"
				/>
				{/* Spire dot */}
				<circle cx="18" cy="7" r="1.8" fill="var(--accent, #4f8cff)" />
				{/* Windows */}
				<rect
					x="7"
					y="18"
					width="5"
					height="8"
					rx="1"
					fill="currentColor"
					fillOpacity="0.6"
				/>
				<rect
					x="15.5"
					y="18"
					width="5"
					height="8"
					rx="1"
					fill="currentColor"
					fillOpacity="0.6"
				/>
				<rect
					x="24"
					y="18"
					width="5"
					height="8"
					rx="1"
					fill="currentColor"
					fillOpacity="0.6"
				/>
				{/* Base */}
				<rect
					x="5"
					y="28"
					width="26"
					height="3.5"
					rx="1"
					fill="currentColor"
					fillOpacity="0.4"
				/>
				{/* Accent glow line */}
				<rect
					x="4"
					y="33"
					width="28"
					height="1"
					rx="0.5"
					fill="var(--accent, #4f8cff)"
					fillOpacity="0.5"
				/>
			</g>

			{/* Text: Model Hotel */}
			<text
				x="69"
				y="33"
				fill="currentColor"
				fontFamily="ui-sans-serif, system-ui, -apple-system, sans-serif"
				fontSize="22"
				fontWeight="700"
				letterSpacing="-0.02em"
			>
				Model
			</text>
			<text
				x="139"
				y="33"
				fill="var(--accent, #4f8cff)"
				fontFamily="ui-sans-serif, system-ui, -apple-system, sans-serif"
				fontSize="22"
				fontWeight="700"
				letterSpacing="-0.02em"
			>
				Hotel
			</text>
		</svg>
	);
});
