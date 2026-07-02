import { useQuery } from "@tanstack/react-query";
import { createContext, useContext, useMemo } from "react";
import { api } from "../api/client";
import type { Me } from "../api/types";

/**
 * Identity of the logged-in caller, resolved from GET /api/auth/me. The
 * sidebar and routes gate on it so a grant-limited user only sees their
 * pages; the server enforces every request regardless, so this is UX, not
 * security.
 */
interface IdentityValue {
	me: Me | null;
	isLoading: boolean;
	isAdmin: boolean;
	/** can reports whether the caller may use a feature (admins always). */
	can: (grant: string) => boolean;
}

const IdentityContext = createContext<IdentityValue>({
	me: null,
	isLoading: false,
	isAdmin: true,
	can: () => true,
});

export function IdentityProvider({ children }: { children: React.ReactNode }) {
	const { data, isLoading, isError } = useQuery({
		queryKey: ["auth-me"],
		queryFn: () => api.auth.me(),
		staleTime: 60_000,
		retry: 1,
	});

	const value = useMemo<IdentityValue>(() => {
		// On a fetch error, fall back to the admin view: an invalid token 401s
		// every other query anyway (surfacing the login screen), and hiding the
		// whole nav on a transient blip would be worse. The server still
		// enforces, so this can never grant real access.
		const me = data ?? null;
		const isAdmin = isError || !me || me.role === "admin";
		return {
			me,
			isLoading,
			isAdmin,
			can: (grant: string) => isAdmin || (me?.grants ?? []).includes(grant),
		};
	}, [data, isLoading, isError]);

	return (
		<IdentityContext.Provider value={value}>
			{children}
		</IdentityContext.Provider>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useIdentity(): IdentityValue {
	return useContext(IdentityContext);
}
