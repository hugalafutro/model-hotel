package provider

import "time"

// OllamaCloudNullableString wraps a string that may be null in the API response.
type OllamaCloudNullableString struct {
	String string `json:"string"`
	Valid  bool   `json:"valid"`
}

// OllamaCloudNullableTime wraps a time that may be null in the API response.
type OllamaCloudNullableTime struct {
	Time  time.Time `json:"time"`
	Valid bool      `json:"valid"`
}

// OllamaCloudAccount represents the response from the Ollama Cloud /api/me endpoint.
type OllamaCloudAccount struct {
	ID                      string                    `json:"id"`
	Email                   string                    `json:"email"`
	Name                    string                    `json:"name"`
	Plan                    string                    `json:"plan"`
	CustomerID              OllamaCloudNullableString `json:"customer_id"`
	SubscriptionID          OllamaCloudNullableString `json:"subscription_id"`
	SubscriptionPeriodStart OllamaCloudNullableTime   `json:"subscription_period_start"`
	SubscriptionPeriodEnd   OllamaCloudNullableTime   `json:"subscription_period_end"`
	SuspendedAt             OllamaCloudNullableTime   `json:"suspended_at"`
}
