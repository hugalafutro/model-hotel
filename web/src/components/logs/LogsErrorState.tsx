interface LogsErrorStateProps {
	message: string;
}

export function LogsErrorState({ message }: LogsErrorStateProps) {
	return (
		<div className="ui-card p-8 text-center">
			<p className="text-red-400 text-sm">{message}</p>
		</div>
	);
}
