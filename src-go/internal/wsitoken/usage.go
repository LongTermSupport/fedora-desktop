package wsitoken

// report is the part of `ccy --token-usage --json` (schema 1) this reads.
type report struct {
	Schema *int    `json:"schema"`
	Error  *string `json:"error"`
	Tokens []entry `json:"tokens"`
}

type entry struct {
	Name     string   `json:"name"`
	File     string   `json:"file"`
	Status   string   `json:"status"`
	Reason   *string  `json:"reason"`
	FiveHour *float64 `json:"five_hour_pct"`
	SevenDay *float64 `json:"seven_day_pct"`
	Resets   struct {
		FiveHour *string `json:"five_hour"`
		SevenDay *string `json:"seven_day"`
	} `json:"resets"`
}
