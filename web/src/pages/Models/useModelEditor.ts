import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { formatPriceInput } from "../../utils/model";

interface UseModelEditorParams {
	model: Model;
	onUpdate: (id: string, updates: Partial<Model>) => void;
}

/** The five editable fields, as the form holds them: strings, never null. */
export interface EditData {
	display_name: string;
	context_length: string;
	max_output_tokens: string;
	input_price_per_million: string;
	output_price_per_million: string;
}

type EditSource = Pick<
	Model,
	| "context_length"
	| "max_output_tokens"
	| "input_price_per_million"
	| "output_price_per_million"
> & { display_name?: string | null };

/** Form values for a model or for its discovered defaults. */
export function editValuesFrom(src: EditSource): EditData {
	return {
		display_name: src.display_name || "",
		context_length: src.context_length?.toString() ?? "",
		max_output_tokens: src.max_output_tokens?.toString() ?? "",
		input_price_per_million: formatPriceInput(src.input_price_per_million),
		output_price_per_million: formatPriceInput(src.output_price_per_million),
	};
}

/** i18n keys for the field names the unsaved-changes dialog lists. */
export const FIELD_LABEL_KEYS: Record<keyof EditData, string> = {
	display_name: "models.detail.displayName",
	context_length: "models.detail.contextLength",
	max_output_tokens: "models.detail.maxOutput",
	input_price_per_million: "models.detail.inputPrice",
	output_price_per_million: "models.detail.outputPrice",
};

export function useModelEditor({ model, onUpdate }: UseModelEditorParams) {
	const { t } = useTranslation();
	const [editing, setEditing] = useState(false);
	const [editVersion, setEditVersion] = useState("");
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);

	const [editData, setEditData] = useState<EditData>(() =>
		editValuesFrom(model),
	);

	// The discovered values a field reverts to, in form (string) shape.
	const discoveredDefaults = useMemo(
		() => editValuesFrom({ ...model, display_name: model.name }),
		[model],
	);

	// Re-sync editData when model changes while editing
	const currentEditVersion = editing ? model.id : "";
	if (editing && currentEditVersion !== editVersion) {
		setEditVersion(currentEditVersion);
		setEditData(editValuesFrom(model));
	}

	const getFieldLabel = (key: string): string =>
		key in FIELD_LABEL_KEYS ? t(FIELD_LABEL_KEYS[key as keyof EditData]) : key;

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
		// An emptied price field is treated as "unchanged", not "clear to null":
		// the API reads a null price as absent (no way to null one price alone),
		// and sending the rest of the edit would pin the stale stored value.
		// Clearing prices is the pin banner's "Reset to source" action instead.
		if (
			editData.input_price_per_million !== "" &&
			Number(editData.input_price_per_million) !==
				(model.input_price_per_million != null
					? Math.round(model.input_price_per_million * 10000) / 10000
					: null)
		)
			fields.push("input_price_per_million");
		if (
			editData.output_price_per_million !== "" &&
			Number(editData.output_price_per_million) !==
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

	/** Drop the pending edits and leave edit mode. */
	const discardEdit = () => {
		setConfirmFields(null);
		setEditing(false);
		setEditData(editValuesFrom(model));
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
		// Prices are only ever sent as numbers: an emptied field never makes the
		// changed list (see getChangedFields), so no null price reaches the API.
		if (changed.includes("input_price_per_million"))
			updates.input_price_per_million = Number(
				editData.input_price_per_million,
			);
		if (changed.includes("output_price_per_million"))
			updates.output_price_per_million = Number(
				editData.output_price_per_million,
			);
		if (Object.keys(updates).length > 0) {
			onUpdate(model.id, updates as Partial<Model>);
		}
		setEditing(false);
	};

	const revertField = (key: keyof EditData) => {
		setEditData((prev) => ({ ...prev, [key]: discoveredDefaults[key] }));
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
		discardEdit,
		handleSave,
		revertField,
	};
}
