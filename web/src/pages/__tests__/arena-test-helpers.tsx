import { screen, waitFor } from "@testing-library/react";
import type { User } from "@testing-library/user-event";
import type { Model } from "../../api/types";
import { mockAllDefaults } from "../../test/helpers";
import { mockModel } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";

export const mockModel2: Model = {
	...mockModel,
	id: "model-002",
	model_id: "test-model-v2",
	display_name: "Test Model v2",
};

export const mockModel3: Model = {
	...mockModel,
	id: "model-003",
	model_id: "test-model-v3",
	display_name: "Test Model v3",
};

export const setupDefaultMocks = () => {
	server.use(...mockAllDefaults());
};

export const waitForArenaLoad = async () => {
	await waitFor(
		() => {
			// Check for the Controls section which indicates the page has loaded
			expect(screen.getByText("Controls")).toBeInTheDocument();
		},
		{ timeout: 3000 },
	);
};

export interface SetupAndRunOptions {
	mode?: "competition" | "compare";
	models?: Model[];
	prompt?: string;
}

export async function setupAndRunArena(
	user: User,
	options: SetupAndRunOptions = {},
): Promise<void> {
	const {
		mode = "competition",
		models: selectedModels = [mockModel, mockModel2],
		prompt = "Test prompt",
	} = options;

	// Toggle to Compare mode if requested
	if (mode === "compare") {
		await user.click(screen.getByRole("button", { name: "Compare" }));
		await waitFor(() => {
			expect(
				screen.getByText(/Side-by-side.*compare model outputs/i),
			).toBeInTheDocument();
		});
	}

	// Wait for models to load, then select them
	await waitFor(
		() => {
			expect(
				screen.getByText(selectedModels[0].display_name),
			).toBeInTheDocument();
		},
		{ timeout: 2000 },
	);
	for (const model of selectedModels) {
		await user.click(screen.getByText(model.display_name));
	}

	// Type prompt
	const textarea = screen.getByRole("textbox", { name: /prompt/i });
	await user.type(textarea, prompt);

	// Click Run Arena
	await user.click(screen.getByRole("button", { name: /Run Arena/i }));

	// Wait for streaming to start (Stop All button appears)
	await waitFor(
		() => {
			expect(
				screen.getByRole("button", { name: "Stop All" }),
			).toBeInTheDocument();
		},
		{ timeout: 2000 },
	);
}
