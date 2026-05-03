package provider

// CoherePricingEntry provides pricing and max output data not available from the API.
type CoherePricingEntry struct {
	ModelID               string  `json:"model_id"`
	DisplayName           string  `json:"display_name"`
	Description           string  `json:"description"`
	MaxOutputTokens       int     `json:"max_output_tokens"`
	InputPricePerMillion  float64 `json:"input_price_per_million"`
	OutputPricePerMillion float64 `json:"output_price_per_million"`
}

var coherePricingCatalog = []CoherePricingEntry{
	{
		ModelID:               "command-a-03-2025",
		DisplayName:           "Command A",
		Description:           "Cohere's flagship model with tool use, reasoning, and vision.",
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.50,
		OutputPricePerMillion: 10.00,
	},
	{
		ModelID:               "command-a-reasoning-08-2025",
		DisplayName:           "Command A Reasoning",
		Description:           "Command A with reasoning mode enabled.",
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.50,
		OutputPricePerMillion: 10.00,
	},
	{
		ModelID:               "command-a-vision-07-2025",
		DisplayName:           "Command A Vision",
		Description:           "Command A with vision capabilities.",
		MaxOutputTokens:       16384,
		InputPricePerMillion:  2.50,
		OutputPricePerMillion: 10.00,
	},
	{
		ModelID:               "command-a-translate-08-2025",
		DisplayName:           "Command A Translate",
		Description:           "Command A specialized for translation tasks.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.04,
		OutputPricePerMillion: 0.15,
	},
	{
		ModelID:               "command-r-plus-08-2024",
		DisplayName:           "Command R+",
		Description:           "Previous generation flagship model with tool use.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  2.50,
		OutputPricePerMillion: 10.00,
	},
	{
		ModelID:               "command-r-08-2024",
		DisplayName:           "Command R",
		Description:           "Previous generation model with tool use.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.15,
		OutputPricePerMillion: 0.60,
	},
	{
		ModelID:               "command-r7b-12-2024",
		DisplayName:           "Command R7B",
		Description:           "Small efficient model with tool use.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.04,
		OutputPricePerMillion: 0.15,
	},
	{
		ModelID:               "command-r7b-arabic-02-2025",
		DisplayName:           "Command R7B Arabic",
		Description:           "Arabic-focused small model.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.04,
		OutputPricePerMillion: 0.15,
	},
	{
		ModelID:               "c4ai-aya-expanse-32b",
		DisplayName:           "Aya Expanse 32B",
		Description:           "Multilingual model supporting 23 languages.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.50,
		OutputPricePerMillion: 1.50,
	},
	{
		ModelID:               "c4ai-aya-vision-32b",
		DisplayName:           "Aya Vision 32B",
		Description:           "Multilingual vision model.",
		MaxOutputTokens:       4096,
		InputPricePerMillion:  0.50,
		OutputPricePerMillion: 1.50,
	},
}

// GetCoherePricingCatalog returns the Cohere pricing catalog.
func GetCoherePricingCatalog() []CoherePricingEntry {
	return coherePricingCatalog
}

// LookupCoherePricing finds a pricing entry by model ID.
func LookupCoherePricing(catalog []CoherePricingEntry, modelID string) *CoherePricingEntry {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}
