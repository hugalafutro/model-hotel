import { Component, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

// Catches a page that throws while loading or rendering, so the sidebar and
// the rest of the app stay up instead of React unmounting to a blank page.
// Callers key it by route so navigating away clears the error.
export class PageErrorBoundary extends Component<
	{ children: ReactNode },
	{ failed: boolean }
> {
	state = { failed: false };

	static getDerivedStateFromError() {
		return { failed: true };
	}

	render() {
		return this.state.failed ? <PageError /> : this.props.children;
	}
}

function PageError() {
	const { t } = useTranslation();
	return (
		<div role="alert" className="ui-card flex flex-col items-start gap-3 p-6">
			<p>{t("common.pageError")}</p>
			<button
				type="button"
				className="ui-btn"
				onClick={() => window.location.reload()}
			>
				{t("common.reloadPage")}
			</button>
		</div>
	);
}
