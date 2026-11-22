package settings

// Upstream is the proxy FormNET's own outgoing connections go through. The
// password is kept here, in a file only the user can read, like the root key.
type Upstream struct {
	Enabled  bool     `json:"enabled"`
	URL      string   `json:"url"`
	Bypass   []string `json:"bypass"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
}

// SetUpstream keeps the upstream proxy choice and writes the whole file.
func (f *File) SetUpstream(u Upstream) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	u.Bypass = append([]string{}, u.Bypass...)
	next.Upstream = &u
	return f.writeLocked(next)
}
