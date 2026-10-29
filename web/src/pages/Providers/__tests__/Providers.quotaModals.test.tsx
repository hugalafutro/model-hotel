import { screen, waitFor } from "@testing-library/react";
import { useQuotaModal } from "../../../context/QuotaModalContext";
import { mockAllDefaults } from "../../../test/helpers";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Providers } from "../../Providers";

// The card's quota badges are stubbed to bare buttons so this file checks only
// which quota modal the page asks for, without each provider's quota fixture.
vi.mock("../../../components/QuotaBadge", async (importOriginal) => ({
	...(await importOriginal<typeof import("../../../components/QuotaBadge")>()),
	QuotaBadges: ({
		onNeuralwattClick,
		onOpenCodeGoClick,
	}: {
		onNeuralwattClick?: () => void;
		onOpenCodeGoClick?: () => void;
	}) => (
		<>
			<button
				type="button"
				data-testid="stub-neuralwatt"
				onClick={onNeuralwattClick}
			/>
			<button
				type="button"
				data-testid="stub-opencode-go"
				onClick={onOpenCodeGoClick}
			/>
		</>
	),
}));

function OpenProbe() {
	const { open } = useQuotaModal();
	return <output data-testid="quota-open">{open ?? ""}</output>;
}

describe("Providers quota modal requests", () => {
	beforeEach(() => {
		server.resetHandlers();
		server.use(...mockAllDefaults());
	});

	it.each([
		["stub-neuralwatt", "neuralwatt"],
		["stub-opencode-go", "opencode-go"],
	])("%s opens the %s quota modal", async (testId, modal) => {
		const { user } = renderWithProviders(
			<>
				<Providers />
				<OpenProbe />
			</>,
		);
		await user.click(await screen.findByTestId(testId));
		await waitFor(() =>
			expect(screen.getByTestId("quota-open")).toHaveTextContent(modal),
		);
	});
});
