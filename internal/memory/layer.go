package memory

// Layer identifies which storage tier a message or summary belongs to (documentation / routing).
type Layer string

const (
	LayerContext       Layer = "context"        // live model context (MessageBuffer)
	LayerTranscript    Layer = "transcript"     // session history for resume/UI
	LayerCrossSession  Layer = "cross_session"  // episodic / topics / long-term memory
)
