import { ChevronLeft, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import {
	daysInMonth,
	firstDayOfMonth,
	pad,
	todayISO,
} from "./AccentCalendar.utils";

/* =========================================================
   Accent-themed inline calendar
   ===================================================== */
export function AccentCalendar({
	initialYear,
	initialMonth,
	from,
	to,
	onSelect,
}: {
	initialYear: number;
	initialMonth: number;
	from: string;
	to: string;
	onSelect: (dateStr: string) => void;
}) {
	const [year, setYear] = useState(initialYear);
	const [month, setMonth] = useState(initialMonth);
	const today = todayISO();

	const days = daysInMonth(year, month);
	const firstDay = firstDayOfMonth(year, month);
	const blanks = firstDay;

	const monthName = useMemo(
		() => new Date(year, month, 1).toLocaleString(undefined, { month: "long" }),
		[year, month],
	);

	const weekdays = useMemo(
		() =>
			Array.from({ length: 7 }, (_, i) => {
				// Use a known Sunday (2024-01-07) as anchor for Sun-first grid
				const date = new Date(2024, 0, 7 + i);
				return new Intl.DateTimeFormat(undefined, {
					weekday: "narrow",
				}).format(date);
			}),
		[],
	);

	const handlePrev = () => {
		if (month === 0) {
			setMonth(11);
			setYear((y) => y - 1);
		} else {
			setMonth((m) => m - 1);
		}
	};

	const handleNext = () => {
		if (month === 11) {
			setMonth(0);
			setYear((y) => y + 1);
		} else {
			setMonth((m) => m + 1);
		}
	};

	const isInRange = (day: number): boolean => {
		if (!from || !to) return false;
		const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
		return dStr >= from && dStr <= to;
	};

	const isStart = (day: number): boolean => {
		if (!from) return false;
		const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
		return dStr === from;
	};

	const isEnd = (day: number): boolean => {
		if (!to) return false;
		const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
		return dStr === to;
	};

	const isSelected = (day: number): boolean => isStart(day) || isEnd(day);

	return (
		<div>
			<div className="flex items-center justify-between mb-3">
				<button
					type="button"
					onClick={handlePrev}
					className="text-gray-400 hover:text-white transition-colors p-1 rounded-(--radius-button) hover:bg-gray-700"
				>
					<ChevronLeft size={16} />
				</button>
				<span className="text-sm font-semibold text-white">
					{monthName} {year}
				</span>
				<button
					type="button"
					onClick={handleNext}
					className="text-gray-400 hover:text-white transition-colors p-1 rounded-(--radius-button) hover:bg-gray-700"
				>
					<ChevronRight size={16} />
				</button>
			</div>
			<div className="grid grid-cols-7 gap-0.5 text-center text-[10px] text-gray-500 mb-1">
				{weekdays.map((d, i) => (
					// biome-ignore lint/suspicious/noArrayIndexKey: weekday names can be duplicates (e.g. "S" for Sun/Sat in English) so index is the only stable key
					<div key={`weekday-${i}`}>{d}</div>
				))}
			</div>
			<div className="grid grid-cols-7 gap-0.5">
				{Array.from({ length: blanks }).map((_, i) => {
					// biome-ignore lint/suspicious/noArrayIndexKey: calendar blanks are static structural placeholders with no stable identifier
					return <div key={`blank-${i}`} />;
				})}
				{Array.from({ length: days }).map((_, i) => {
					const day = i + 1;
					const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
					const inRange = isInRange(day);
					const sel = isSelected(day);
					const isToday = dStr === today;

					return (
						<button
							key={day}
							type="button"
							onClick={() => onSelect(dStr)}
							className={`
                                text-[11px] w-7 h-7                                 rounded-(--radius-button) flex items-center justify-center transition-colors
                                ${
																	sel
																		? "bg-(--accent) text-white font-semibold"
																		: inRange
																			? "bg-(--accent)/20 text-(--accent)"
																			: isToday
																				? "border border-(--accent)/50 text-(--accent)"
																				: "text-gray-300 hover:bg-gray-700"
																}
                            `}
						>
							{day}
						</button>
					);
				})}
			</div>
		</div>
	);
}
