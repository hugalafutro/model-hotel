import { useMemo, useState } from "react";
import type { Model } from "../../api/types";
import { formatPriceInput } from "../../utils/model";

interface UseModelEditorParams {
	model: Model;
	onUpdate: (id: string, updates: Partial<Model>) => void;
}

export function useModelEditor({ model, onUpdate }: UseModelEditorParams) {
	const [editing, setEditing] = useState(false);
	const [editVersion, setEditVersion] = useState("");
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);

	const [editData, setEditData] = useState({
		display_name: model.display_name || "",
		context_length: model.context_length?.toString() || "",
		max_output_tokens: model.max_output_tokens?.toString() || "",
		input_price_per_million: formatPriceInput(model.input_price_per_million),
		output_price_per_million: formatPriceInput(model.output_price_per_million),
	});

	const discoveredDefaults = useMemo(
		() => ({
			display_name: model.name || "",
			context_length: model.context_length,
			max_output_tokens: model.max_output_tokens,
			input_price_per_million: model.input_price_per_million,
			output_price_per_million: model.output_price_per_million,
		}),
		[model],
	);

	// Re-sync editData when model changes while editing
	const currentEditVersion = editing ? model.id : "";
	if (editing && currentEditVersion !== editVersion) {
		setEditVersion(currentEditVersion);
		setEditData({
			display_name: model.display_name || "",
			context_length: model.context_length?.toString() || "",
			max_output_tokens: model.max_output_tokens?.toString() || "",
			input_price_per_million: formatPriceInput(model.input_price_per_million),
			output_price_per_million: formatPriceInput(
				model.output_price_per_million,
			),
		});
	}

	const getFieldLabel = (key: string): string => {
		const labels: Record<string, string> = {
			display_name: "Display Name",
			context_length: "Context Length",
			max_output_tokens: "Max Output Tokens",
			input_price_per_million: "Input Price",
			output_price_per_million: "Output Price",
		};
		return labels[key] || key;
	};

	const getChangedFields = (): string[] => {
		const fields: string[] = [];
		if (editData.display_name !== (model.display_name || ""))
			fields.push("display_name");
		const cl =
			editData.context_length === "" ? null : Number(editData.context_length);
		if (cl !== model.context_length) fields.push("context_length");
		const mot =
			editData.max_output_tokens === ""
				? null
				: Number(editData.max_output_tokens);
		if (mot !== model.max_output_tokens) fields.push("max_output_tokens");
		const ipm =
			editData.input_price_per_million === ""
				? null
				: Number(editData.input_price_per_million);
		if (
			ipm !==
			(model.input_price_per_million != null
				? Math.round(model.input_price_per_million * 10000) / 10000
				: null)
		)
			fields.push("input_price_per_million");
		const opm =
			editData.output_price_per_million === ""
				? null
				: Number(editData.output_price_per_million);
		if (
			opm !==
			(model.output_price_per_million != null
				? Math.round(model.output_price_per_million * 10000) / 10000
				: null)
		)
			fields.push("output_price_per_million");
		return fields;
	};

	const handleCancelEdit = () => {
		const changed = getChangedFields();
		if (changed.length > 0) {
			setConfirmFields(changed.map(getFieldLabel));
		} else {
			setEditing(false);
		}
	};

	const handleSave = () => {
		const changed = getChangedFields();
		if (changed.length === 0) {
			setEditing(false);
			return;
		}
		const updates: Record<string, unknown> = {};
		if (changed.includes("display_name"))
			updates.display_name = editData.display_name.trim();
		if (changed.includes("context_length"))
			updates.context_length =
				editData.context_length === "" ? null : Number(editData.context_length);
		if (changed.includes("max_output_tokens"))
			updates.max_output_tokens =
				editData.max_output_tokens === ""
					? null
					: Number(editData.max_output_tokens);
		if (changed.includes("input_price_per_million"))
			updates.input_price_per_million =
				editData.input_price_per_million === ""
					? null
					: Number(editData.input_price_per_million);
		if (changed.includes("output_price_per_million"))
			updates.output_price_per_million =
				editData.output_price_per_million === ""
					? null
					: Number(editData.output_price_per_million);
		if (Object.keys(updates).length > 0) {
			onUpdate(model.id, updates as Partial<Model>);
		}
		setEditing(false);
	};

	const revertField = (key: keyof typeof discoveredDefaults) => {
		if (key === "display_name") {
			setEditData((prev) => ({
				...prev,
				display_name: discoveredDefaults.display_name,
			}));
		} else if (key === "context_length") {
			setEditData((prev) => ({
				...prev,
				context_length: discoveredDefaults.context_length?.toString() ?? "",
			}));
		} else if (key === "max_output_tokens") {
			setEditData((prev) => ({
				...prev,
				max_output_tokens:
					discoveredDefaults.max_output_tokens?.toString() ?? "",
			}));
		} else if (key === "input_price_per_million") {
			setEditData((prev) => ({
				...prev,
				input_price_per_million: formatPriceInput(
					discoveredDefaults.input_price_per_million,
				),
			}));
		} else if (key === "output_price_per_million") {
			setEditData((prev) => ({
				...prev,
				output_price_per_million: formatPriceInput(
					discoveredDefaults.output_price_per_million,
				),
			}));
		}
	};

	return {
		editing,
		setEditing,
		editData,
		setEditData,
		confirmFields,
		setConfirmFields,
		discoveredDefaults,
		getFieldLabel,
		getChangedFields,
		handleCancelEdit,
		handleSave,
		revertField,
	};
}
