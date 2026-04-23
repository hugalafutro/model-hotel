export function Logo({ className = "" }: { className?: string }) {
    return (
        <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 180 40"
            className={className}
            fill="none"
            aria-label="Model Hotel"
        >
            {/* Hotel Building Icon */}
            <g transform="translate(0, 4)">
                {/* Roof */}
                <path
                    d="M16 2L4 12v2h24v-2L16 2z"
                    fill="currentColor"
                    fillOpacity="0.9"
                />
                {/* Spire dot */}
                <circle cx="16" cy="6" r="1.5" fill="var(--accent, #4f8cff)" />
                {/* Windows */}
                <rect
                    x="7"
                    y="16"
                    width="4"
                    height="7"
                    rx="1"
                    fill="currentColor"
                    fillOpacity="0.6"
                />
                <rect
                    x="14"
                    y="16"
                    width="4"
                    height="7"
                    rx="1"
                    fill="currentColor"
                    fillOpacity="0.6"
                />
                <rect
                    x="21"
                    y="16"
                    width="4"
                    height="7"
                    rx="1"
                    fill="currentColor"
                    fillOpacity="0.6"
                />
                {/* Base */}
                <rect
                    x="5"
                    y="25"
                    width="22"
                    height="3"
                    rx="1"
                    fill="currentColor"
                    fillOpacity="0.4"
                />
                {/* Accent glow line */}
                <rect
                    x="4"
                    y="29"
                    width="24"
                    height="1"
                    rx="0.5"
                    fill="var(--accent, #4f8cff)"
                    fillOpacity="0.5"
                />
            </g>

            {/* Text: Model Hotel */}
            <text
                x="40"
                y="26"
                fill="currentColor"
                fontFamily="ui-sans-serif, system-ui, -apple-system, sans-serif"
                fontSize="18"
                fontWeight="700"
                letterSpacing="-0.02em"
            >
                Model
            </text>
            <text
                x="90"
                y="26"
                fill="var(--accent, #4f8cff)"
                fontFamily="ui-sans-serif, system-ui, -apple-system, sans-serif"
                fontSize="18"
                fontWeight="700"
                letterSpacing="-0.02em"
            >
                Hotel
            </text>
        </svg>
    );
}
