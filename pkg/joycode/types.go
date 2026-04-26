package joycode

// ModelInfo describes a JoyCode AI model.
type ModelInfo struct {
	Label              string   `json:"label"`
	ChatAPIModel       string   `json:"chatApiModel"`
	MaxTotalTokens     int      `json:"maxTotalTokens"`
	RespMaxTokens      int      `json:"respMaxTokens"`
	Temperature        float64  `json:"temperature"`
	Features           []string `json:"features"`
	SupportStream      bool     `json:"supportStream"`
	VerificationStatus string   `json:"verificationStatus"`
	ModelID            string   `json:"modelId"`
	CreateTime         int64    `json:"createTime"`
}
