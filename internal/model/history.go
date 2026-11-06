package model

// ToolMetadata contains explicitly projected call headers, never altered
// NativeRecord values. Raw details remain available through scoped trace queries.
type ToolMetadata struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Seq        int64             `json:"seq"`
	Parent     *string           `json:"parent,omitempty"`
	Time       *int64            `json:"time,omitempty"`
	Ended      *int64            `json:"ended,omitempty"`
	Complete   bool              `json:"complete"`
	Failed     bool              `json:"failed"`
	ChildCount int               `json:"child_count"`
	Args       map[string]string `json:"args"`
}
type MessageMetadata struct {
	Seq          int64  `json:"seq"`
	Time         *int64 `json:"time,omitempty"`
	Step         *int64 `json:"step,omitempty"`
	Turn         *int64 `json:"turn,omitempty"`
	Text         string `json:"text"`
	HasReasoning bool   `json:"has_reasoning"`
}
type AttemptMetadata struct {
	ID           string `json:"id"`
	Step         *int64 `json:"step,omitempty"`
	Turn         *int64 `json:"turn,omitempty"`
	EndedSeq     *int64 `json:"ended_seq,omitempty"`
	Text         string `json:"text"`
	HasReasoning bool   `json:"has_reasoning"`
}
type PendingToolMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type TurnMetadata struct {
	Tools        []ToolMetadata        `json:"tools"`
	Messages     []MessageMetadata     `json:"messages"`
	PendingTools []PendingToolMetadata `json:"pending_tools"`
	Attempts     []AttemptMetadata     `json:"attempts"`
	Questions    []UserQuestion        `json:"questions"`
	Outbox       []Outbox              `json:"outbox"`
	Cursor       TraceCursor           `json:"cursor"`
}

// MetadataFrame carries only scalar activity changes, not a raw frame payload.
type MetadataFrame struct {
	ID                            int64
	Type, AttemptID               string
	Step, Turn, EndedSeq          *int64
	ChunkType, Text, CallID, Name string
}
