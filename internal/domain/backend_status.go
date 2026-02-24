package domain

// BackendStatus describes backend connectivity state to rTorrent.
type BackendStatus struct {
	Kind    string
	Message string
}
