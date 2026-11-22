package flow

// MessageSummary counts the WebSocket messages of one flow. The messages
// themselves travel only through GET /api/flows/{id}/messages.
type MessageSummary struct {
	Count       uint64 `json:"count"`                  // every message seen, kept or not
	Bytes       int64  `json:"bytes"`                  // payload bytes of every message as sent (compressed when compressed)
	Dropped     uint64 `json:"dropped"`                // messages counted but not kept: past 1000, or past 1 MiB of payload
	Deflate     bool   `json:"deflate,omitempty"`      // permessage-deflate was negotiated
	CloseCode   int    `json:"close_code,omitempty"`   // from the first close frame
	CloseReason string `json:"close_reason,omitempty"` // from the first close frame
	ClosedBy    string `json:"closed_by,omitempty"`    // client or server: who sent the first close frame
	ParseError  string `json:"parse_error,omitempty"`  // framing FormNET could not follow; the bytes were still relayed unchanged
}
