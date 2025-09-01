package events

type Event = string

const (
	WorkspaceChange Event = "aerospace_workspace_change"
	WindowCreated   Event = "aerospace_window_created"
	WindowDestroyed Event = "aerospace_window_destroyed"
	WindowMoved     Event = "aerospace_window_moved"
	AerospaceRefresh Event = "aerospace_refresh"
)

type WorkspaceChangeEventInfo struct {
	Focused string `json:"focused"`
	Prev    string `json:"prev"`
}
