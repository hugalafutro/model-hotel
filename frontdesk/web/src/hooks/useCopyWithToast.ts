import { useTranslation } from "react-i18next";
import { useToast } from "../context/ToastContext";
import { useCopyToClipboard } from "./useCopyToClipboard";

// useCopyWithToast copies one value and says which way it went, for the places
// that report the result as a toast rather than as a label on the button (so
// the hook's "Copied" flag is left off). A refused clipboard toasts the failure
// rather than passing silently, because the operator is mid-task and has to
// know to select the text by hand.
export function useCopyWithToast(): (value: string, okMessage: string) => void {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { copy } = useCopyToClipboard({ trackCopied: false });
	return (value: string, okMessage: string) => {
		void copy(value).then((ok) => {
			toast(
				ok ? okMessage : t("common.failedToCopy"),
				ok ? "success" : "error",
			);
		});
	};
}
