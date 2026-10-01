package provider

import "net/http"

// bearerHeader builds the request headers for a Bearer-authenticated fetch,
// omitting the header entirely when the server needs no key (a local
// KoboldCPP or LM Studio started without --password).
func bearerHeader(apiKey string) http.Header {
	h := http.Header{}
	if apiKey != "" {
		h.Set("Authorization", "Bearer "+apiKey)
	}
	return h
}

// bearerJSONHeader is the header set a provider listing call sends: the Bearer
// credential and a JSON content type. Unlike bearerHeader it sets Authorization
// even for an empty key, so a keyless call sends a bare "Bearer ".
func bearerJSONHeader(apiKey string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+apiKey)
	h.Set("Content-Type", "application/json")
	return h
}
