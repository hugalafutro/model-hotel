package provider

type ZAIModelSpec struct {
	ModelID         string
	ContextLength   int
	MaxOutputTokens int
	Modality        string
	Reasoning       bool
	ToolCalling     bool
	StructuredOutput bool
}

var zaiCatalog = []ZAIModelSpec{
	{"glm-5.1", 200000, 131072, "text", true, true, true},
	{"glm-5", 200000, 131072, "text", true, true, true},
	{"glm-5-turbo", 200000, 131072, "text", true, true, true},
	{"glm-4.7", 200000, 131072, "text", true, true, true},
	{"glm-4.7-flash", 200000, 131072, "text", true, true, true},
	{"glm-4.7-flashx", 200000, 131072, "text", true, true, true},
	{"glm-4.6", 200000, 131072, "text", true, true, false},
	{"glm-4.5", 128000, 98304, "text", true, true, true},
	{"glm-4.5-air", 128000, 98304, "text", true, true, true},
	{"glm-4.5-x", 128000, 98304, "text", true, true, true},
	{"glm-4.5-airx", 128000, 98304, "text", true, true, true},
	{"glm-4.5-flash", 128000, 98304, "text", true, true, true},
	{"glm-4-32b-0414-128k", 128000, 16384, "text", false, true, true},
	{"glm-5v-turbo", 200000, 131072, "vision", true, true, false},
	{"glm-4.6v", 128000, 32768, "vision", true, true, false},
	{"glm-4.6v-flash", 128000, 32768, "vision", true, true, false},
	{"glm-4.6v-flashx", 128000, 32768, "vision", true, true, false},
	{"glm-4.5v", 128000, 16384, "vision", true, false, false},
}

func GetZAIModels() []ZAIModelSpec {
	return zaiCatalog
}
