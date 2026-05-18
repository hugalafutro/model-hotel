import { useEffect, useRef, useState } from "react";
import { todayISO, toISODate } from "../components/AccentCalendar.utils";

/* =========================================================
   Shared date range picker state and logic
   ===================================================== */
export function useDateRangePicker(onFilterChange?: () => void) {
	const [dateFrom, setDateFrom] = useState("");
	const [dateTo, setDateTo] = useState("");
	const [showDatePicker, setShowDatePicker] = useState(false);
	const [pendingFrom, setPendingFrom] = useState("");
	const [pendingTo, setPendingTo] = useState("");
	const datePickerRef = useRef<HTMLDivElement>(null);

	useEffect(() => {
		function handleClickOutside(e: MouseEvent) {
			if (
				datePickerRef.current &&
				!datePickerRef.current.contains(e.target as Node)
			) {
				setShowDatePicker(false);
			}
		}
		if (showDatePicker) {
			document.addEventListener("mousedown", handleClickOutside);
			return () =>
				document.removeEventListener("mousedown", handleClickOutside);
		}
	}, [showDatePicker]);

	const handleCalendarSelect = (dStr: string) => {
		if (!pendingFrom || (pendingFrom && pendingTo)) {
			setPendingFrom(dStr);
			setPendingTo("");
		} else if (dStr < pendingFrom) {
			setPendingTo(pendingFrom);
			setPendingFrom(dStr);
		} else {
			setPendingTo(dStr);
		}
	};

	const applyDateFilter = () => {
		if (pendingFrom) {
			// Construct dates in the browser's local timezone so the filter
			// range matches what the user sees via toLocaleString() rather
			// than UTC (which would shift near midnight).
			setDateFrom(new Date(`${pendingFrom}T00:00:00`).toISOString());
			if (pendingTo && pendingTo >= pendingFrom) {
				setDateTo(new Date(`${pendingTo}T23:59:59.999`).toISOString());
			} else {
				setDateTo(new Date(`${pendingFrom}T23:59:59.999`).toISOString());
			}
		} else {
			setDateFrom("");
			setDateTo("");
		}
		setShowDatePicker(false);
		onFilterChange?.();
	};

	const clearDateFilter = () => {
		setDateFrom("");
		setDateTo("");
		setPendingFrom("");
		setPendingTo("");
		setShowDatePicker(false);
		onFilterChange?.();
	};

	const toggleDatePicker = () => {
		if (!showDatePicker) {
			// Use toISODate to extract the local date portion from ISO
			// timestamps, consistent with the calendar's local-date model.
			setPendingFrom(dateFrom ? toISODate(new Date(dateFrom)) : "");
			setPendingTo(dateTo ? toISODate(new Date(dateTo)) : "");
		}
		setShowDatePicker((s) => !s);
	};

	const closeDatePicker = () => setShowDatePicker(false);

	const hasDateFilter = !!dateFrom && !!dateTo;

	const now = new Date();
	// Append T00:00:00 so the date-only string is parsed as local time
	// rather than UTC (per ECMAScript spec, bare "YYYY-MM-DD" parses as
	// UTC midnight, which shifts the month on the 1st in UTC-X offsets).
	const pickerYear = showDatePicker
		? new Date(`${pendingFrom || todayISO()}T00:00:00`).getFullYear()
		: now.getFullYear();
	const pickerMonth = showDatePicker
		? new Date(`${pendingFrom || todayISO()}T00:00:00`).getMonth()
		: now.getMonth();

	return {
		dateFrom,
		dateTo,
		showDatePicker,
		pendingFrom,
		pendingTo,
		datePickerRef,
		hasDateFilter,
		pickerYear,
		pickerMonth,
		handleCalendarSelect,
		applyDateFilter,
		clearDateFilter,
		toggleDatePicker,
		closeDatePicker,
	};
}
