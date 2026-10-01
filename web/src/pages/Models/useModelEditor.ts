import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { CAP_META, type CapKey } from "../../components/capMeta";
import {
	formatPriceInput,
	parseCapabilities,
	roundPrice,
} from "../../utils/model";

interface UseModelEditorParams {
	model: Model;
	onUpdate: (id: string, updates: Partial<Model>) => void;
}

/** The six editable fields, as the form holds them: strings, never null. */
export interface EditData {
	display_name: string;
	context_length: string;
	max_output_tokens: string;
	input_price_per_million: string;
	output_price_per_million: string;
	search_price_per_thousand: string;
}

/** The price fields, edited the same way; a rerank model shows only the last. */
export type PriceField =
	| "input_price_per_million"
	| "output_price_per_million"
	| "search_price_per_thousand";

type EditSource = Pick<
	Model,
	| "context_length"
	| "max_output_tokens"
	| "input_price_per_million"
	| "output_price_per_million"
	| "search_price_per_thousand"
> & { display_name?: string | null };

/** Form values for a model or for its discovered defaults. */
export function editValuesFrom(src: EditSource): EditData {
	return {
		display_name: src.display_name || "",
		context_length: src.context_length?.toString() ?? "",
		max_output_tokens: src.max_output_tokens?.toString() ?? "",
		input_price_per_million: formatPriceInput(src.input_price_per_million),
		output_price_per_million: formatPriceInput(src.output_price_per_million),
		search_price_per_thousand: formatPriceInput(src.search_price_per_thousand),
	};
}

/** i18n keys for the field names the unsaved-changes dialog lists. */
export const FIELD_LABEL_KEYS: Record<keyof EditData, string> = {
	display_name: "models.detail.displayName",
	context_length: "models.detail.contextLength",
	max_output_tokens: "models.detail.maxOutput",
	input_price_per_million: "models.detail.inputPrice",
	output_price_per_million: "models.detail.outputPrice",
	search_price_per_thousand: "models.detail.searchPrice",
};

/** The capability flags an operator can switch, as stored. */
export function editCapsFrom(model: Pick<Model, "capabilities">) {
	const caps = parseCapabilities(model.capabilities);
	return Object.fromEntries(
		CAP_META.map((m) => [m.key, Boolean(caps[m.key])]),
	) as Record<CapKey, boolean>;
}

export function useModelEditor({ model, onUpdate }: UseModelEditorParams) {
	const { t } = useTranslation();
	const [editing, setEditing] = useState(false);
	const [editVersion, setEditVersion] = useState("");
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);

	const [editData, setEditData] = useState<EditData>(() =>
		editValuesFrom(model),
	);
	// Only a custom or self-hosted provider's capabilities are the operator's
	// to set; the backend decides which, and every other type's come from the
	// vendor's own API or data.
	const capsEditable = model.capabilities_editable === true;
	const [editCaps, setEditCaps] = useState(() => editCapsFrom(model));
	const toggleCap = (key: CapKey) =>
		setEditCaps((prev) => ({ ...prev, [key]: !prev[key] }));

	// The discovered values a field reverts to, in form (string) shape.
	const discoveredDefaults = useMemo(
		() => editValuesFrom({ ...model, display_name: model.name }),
		[model],
	);

	/** Reset the form to the model's current values. */
	const reseed = () => {
		setEditData(editValuesFrom(model));
		setEditCaps(editCapsFrom(model));
	};

	// Re-sync editData when model changes while editing
	const currentEditVersion = editing ? model.id : "";
	if (editing && currentEditVersion !== editVersion) {
		setEditVersion(currentEditVersion);
		reseed();
	}
	// Outside edit mode the form follows the model, so a change made elsewhere
	// (a price reset to source after a save in this same modal) is what the
	// next edit starts from, not the values typed last time. Otherwise the
	// stale prices counted as edits and the next save re-pinned them.
	const seedKey = JSON.stringify([editValuesFrom(model), editCapsFrom(model)]);
	const [seededKey, setSeededKey] = useState(seedKey);
	if (!editing && seedKey !== seededKey) {
		setSeededKey(seedKey);
		reseed();
	}

	const getFieldLabel = (key: string): string =>
		key === "capabilities"
			? t("models.detail.capabilities")
			: key in FIELD_LABEL_KEYS
				? t(FIELD_LABEL_KEYS[key as keyof EditData])
				: key;

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
		const priceChanged = (field: PriceField) => {
			const stored = model[field];
			return (
				editData[field] !== "" &&
				Number(editData[field]) !== (stored != null ? roundPrice(stored) : null)
			);
		};
		for (const field of [
			"input_price_per_million",
			"output_price_per_million",
			"search_price_per_thousand",
		] as const) {
			if (priceChanged(field)) fields.push(field);
		}
		if (
			capsEditable &&
			JSON.stringify(editCaps) !== JSON.stringify(editCapsFrom(model))
		)
			fields.push("capabilities");
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
		reseed();
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
		if (changed.includes("search_price_per_thousand"))
			updates.search_price_per_thousand = Number(
				editData.search_price_per_thousand,
			);
		// The API replaces capabilities whole, so the flags the form does not
		// show (streaming) go back as stored.
		if (changed.includes("capabilities"))
			updates.capabilities = {
				...parseCapabilities(model.capabilities),
				...editCaps,
			};
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
		capsEditable,
		editCaps,
		toggleCap,
	};
}
