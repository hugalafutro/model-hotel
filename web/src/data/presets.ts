export interface PersonaPreset {
	id: string;
	icon: string;
	label: string;
	systemPrompt: string;
}

export interface ArenaPromptPreset {
	id: string;
	icon: string;
	label: string;
	prompt: string;
}

export const CHAT_PERSONAS: PersonaPreset[] = [
	{
		id: "merlin",
		icon: "🧙",
		label: "presets.personas.merlin.label",
		systemPrompt: "presets.personas.merlin.prompt",
	},
	{
		id: "madame-vex",
		icon: "🔮",
		label: "presets.personas.madame-vex.label",
		systemPrompt: "presets.personas.madame-vex.prompt",
	},
	{
		id: "sarge",
		icon: "🦾",
		label: "presets.personas.sarge.label",
		systemPrompt: "presets.personas.sarge.prompt",
	},
	{
		id: "auntie-wei",
		icon: "🍵",
		label: "presets.personas.auntie-wei.label",
		systemPrompt: "presets.personas.auntie-wei.prompt",
	},
	{
		id: "grimm",
		icon: "💀",
		label: "presets.personas.grimm.label",
		systemPrompt: "presets.personas.grimm.prompt",
	},
	{
		id: "kairos",
		icon: "🎙️",
		label: "presets.personas.kairos.label",
		systemPrompt: "presets.personas.kairos.prompt",
	},
	{
		id: "phreak",
		icon: "📡",
		label: "presets.personas.phreak.label",
		systemPrompt: "presets.personas.phreak.prompt",
	},
	{
		id: "roux",
		icon: "🍳",
		label: "presets.personas.roux.label",
		systemPrompt: "presets.personas.roux.prompt",
	},
	{
		id: "unit-734",
		icon: "🤖",
		label: "presets.personas.unit-734.label",
		systemPrompt: "presets.personas.unit-734.prompt",
	},
	{
		id: "bramble",
		icon: "🌳",
		label: "presets.personas.bramble.label",
		systemPrompt: "presets.personas.bramble.prompt",
	},
	{
		id: "nera",
		icon: "📰",
		label: "presets.personas.nera.label",
		systemPrompt: "presets.personas.nera.prompt",
	},
	{
		id: "oolo",
		icon: "🃏",
		label: "presets.personas.oolo.label",
		systemPrompt: "presets.personas.oolo.prompt",
	},
	{
		id: "dr-maren",
		icon: "🔭",
		label: "presets.personas.dr-maren.label",
		systemPrompt: "presets.personas.dr-maren.prompt",
	},
	{
		id: "nonna-pia",
		icon: "🍝",
		label: "presets.personas.nonna-pia.label",
		systemPrompt: "presets.personas.nonna-pia.prompt",
	},
	{
		id: "whisper",
		icon: "🔤",
		label: "presets.personas.whisper.label",
		systemPrompt: "presets.personas.whisper.prompt",
	},
];

export const ARENA_PROMPTS: ArenaPromptPreset[] = [
	{
		id: "dilemma",
		icon: "🧩",
		label: "presets.prompts.dilemma.label",
		prompt: "presets.prompts.dilemma.text",
	},
	{
		id: "lore",
		icon: "📜",
		label: "presets.prompts.lore.label",
		prompt: "presets.prompts.lore.text",
	},
	{
		id: "hook",
		icon: "🎣",
		label: "presets.prompts.hook.label",
		prompt: "presets.prompts.hook.text",
	},
	{
		id: "blueprint",
		icon: "🏗️",
		label: "presets.prompts.blueprint.label",
		prompt: "presets.prompts.blueprint.text",
	},
	{
		id: "spiral",
		icon: "🌀",
		label: "presets.prompts.spiral.label",
		prompt: "presets.prompts.spiral.text",
	},
	{
		id: "trolley",
		icon: "⚖️",
		label: "presets.prompts.trolley.label",
		prompt: "presets.prompts.trolley.text",
	},
	{
		id: "algorithm",
		icon: "💻",
		label: "presets.prompts.algorithm.label",
		prompt: "presets.prompts.algorithm.text",
	},
	{
		id: "paradox",
		icon: "🪞",
		label: "presets.prompts.paradox.label",
		prompt: "presets.prompts.paradox.text",
	},
	{
		id: "integral",
		icon: "📐",
		label: "presets.prompts.integral.label",
		prompt: "presets.prompts.integral.text",
	},
	{
		id: "contract",
		icon: "📄",
		label: "presets.prompts.contract.label",
		prompt: "presets.prompts.contract.text",
	},
	{
		id: "cipher",
		icon: "🔐",
		label: "presets.prompts.cipher.label",
		prompt: "presets.prompts.cipher.text",
	},
	{
		id: "eulogy",
		icon: "🕯️",
		label: "presets.prompts.eulogy.label",
		prompt: "presets.prompts.eulogy.text",
	},
	{
		id: "fork",
		icon: "🔀",
		label: "presets.prompts.fork.label",
		prompt: "presets.prompts.fork.text",
	},
	{
		id: "mosaic",
		icon: "🧫",
		label: "presets.prompts.mosaic.label",
		prompt: "presets.prompts.mosaic.text",
	},
	{
		id: "heist",
		icon: "🎭",
		label: "presets.prompts.heist.label",
		prompt: "presets.prompts.heist.text",
	},
];
