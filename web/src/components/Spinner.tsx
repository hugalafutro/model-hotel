import { useEffect, useState } from "react";
import { useTheme } from "../context/ThemeContext";

const BRAILLE = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

export function Spinner({ className = "" }: { className?: string }) {
	const { uiStyle } = useTheme();
	const [frame, setFrame] = useState(0);

	useEffect(() => {
		const id = setInterval(() => setFrame((f) => (f + 1) % BRAILLE.length), 80);
		return () => clearInterval(id);
	}, []);

	if (uiStyle === "cyber-terminal") {
		return (
			<span className={`inline-block w-[1ch] text-center ${className}`}>
				{BRAILLE[frame]}
			</span>
		);
	}

	return (
		<span
			className={`inline-block w-3 h-3 border-2 border-current/30 border-t-current rounded-full animate-spin ${className}`}
		/>
	);
}
