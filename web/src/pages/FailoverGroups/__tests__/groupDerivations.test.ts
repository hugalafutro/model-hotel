import { describe, expect, it } from "vitest";
import type { FailoverEntry, FailoverGroup } from "../../../api/types";
import { mockFailoverGroup } from "../../../test/mocks/data";
import { entryToggleUpdate } from "../groupDerivations";

const member = (
	uuid: string,
	enabled = true,
	providerEnabled = true,
): FailoverEntry => ({
	model_uuid: uuid,
	model_id: "glm-5.3",
	provider_id: `p-${uuid}`,
	provider_name: uuid,
	display_name: uuid,
	enabled,
	model_enabled: true,
	provider_enabled: providerEnabled,
	disabled_manually: false,
	context_length: null,
	owned_by: "",
});

const groupWith = (
	groupEnabled: boolean,
	entries: FailoverEntry[],
	autoDisabled = false,
): FailoverGroup => ({
	...mockFailoverGroup,
	group_enabled: groupEnabled,
	auto_disabled: autoDisabled,
	entries,
});

describe("entryToggleUpdate", () => {
	it("disables a group that drops below two routable members", () => {
		const group = groupWith(true, [member("a"), member("b"), member("c")]);
		expect(entryToggleUpdate(group, { a: true, b: false, c: false })).toEqual({
			entry_enabled: { a: true, b: false, c: false },
			group_enabled: false,
			floor_disabled: true,
		});
	});

	it("re-enables a group the floor took down once it regains two", () => {
		// Off and stamped auto_disabled: the floor (or discovery), not the operator.
		const group = groupWith(false, [member("a"), member("b", false)], true);
		expect(entryToggleUpdate(group, { a: true, b: true })).toEqual({
			entry_enabled: { a: true, b: true },
			group_enabled: true,
		});
	});

	it("leaves a hand-disabled group off when a toggle keeps it viable", () => {
		// Off without the stamp: the operator switched it off. Switching one
		// member off keeps two, which must not switch it back on.
		const group = groupWith(false, [member("a"), member("b"), member("c")]);
		expect(entryToggleUpdate(group, { a: true, b: true, c: false })).toEqual({
			entry_enabled: { a: true, b: true, c: false },
		});
	});

	it("leaves a hand-disabled group off through a dip below two and back", () => {
		// Off by hand at two members; a toggle drops it to one, the next
		// restores two. The member count says nothing, the stamp does.
		const dipped = groupWith(false, [member("a"), member("b", false)]);
		expect(entryToggleUpdate(dipped, { a: true, b: true })).toEqual({
			entry_enabled: { a: true, b: true },
		});
	});

	it("sends only the entry flags when nothing about the floor changes", () => {
		const group = groupWith(true, [member("a"), member("b"), member("c")]);
		expect(entryToggleUpdate(group, { a: true, b: true, c: false })).toEqual({
			entry_enabled: { a: true, b: true, c: false },
		});
	});
});
