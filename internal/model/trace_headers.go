package model

// EventHeader is a query projection, not a truncated native record.
type EventHeader struct {
	Seq  int64  `json:"seq"`
	Type string `json:"type"`
	Time *int64 `json:"time,omitempty"`
}

type FrameHeader struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	ChunkType string `json:"chunk_type,omitempty"`
	AttemptID string `json:"attempt_id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

type TraceHeaders struct {
	Events  []EventHeader `json:"events"`
	Frames  []FrameHeader `json:"frames"`
	HasMore bool          `json:"has_more"`
	Before  *int64        `json:"before"`
	Cursor  TraceCursor   `json:"cursor"`
}
