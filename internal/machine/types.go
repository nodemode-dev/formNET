// Package machine watches what every program on this computer is connected
// to, not only what went through the proxy: the connection table, the owning
// processes and, where the platform reports it, bytes per process.
//
// Sources by platform:
//
//	macOS    lsof -nP -i (connections and owners), nettop (bytes per process)
//	Linux    /proc/net/tcp, tcp6, udp, udp6 and /proc/<pid>/fd (owners)
//	Windows  netstat -ano and tasklist
//
// Reading the table needs no privilege; what a platform hides from an
// unprivileged user (other users' processes) stays hidden here too.
package machine

import "time"

// Process is one program with at least one connection.
type Process struct {
	PID         int    `json:"pid"`
	Name        string `json:"name"`
	Path        string `json:"path,omitempty"`
	User        string `json:"user,omitempty"`
	BytesIn     int64  `json:"bytes_in"`  // cumulative since the process started, 0 when unknown
	BytesOut    int64  `json:"bytes_out"` // cumulative since the process started, 0 when unknown
	Connections int    `json:"connections"`
}

// Connection is one socket with a remote end.
type Connection struct {
	Proto   string `json:"proto"` // tcp, tcp6, udp, udp6
	PID     int    `json:"pid"`
	Process string `json:"process"`        // the name the process reports for itself
	Path    string `json:"path,omitempty"` // its executable, when the platform tells
	Local   string `json:"local"`          // ip:port
	Remote  string `json:"remote"`         // ip:port; empty for a listening socket
	// RemoteHost is the reverse DNS name, only when reverse DNS is turned on.
	// RemoteHostVerified is true when a forward lookup of that name gave the IP back.
	RemoteHost         string    `json:"remote_host,omitempty"`
	RemoteHostVerified bool      `json:"remote_host_verified,omitempty"`
	State              string    `json:"state"` // ESTABLISHED, LISTEN, ...
	FirstSeen          time.Time `json:"first_seen"`
	LastSeen           time.Time `json:"last_seen"`
}

// Snapshot is the machine's connection table at one instant.
type Snapshot struct {
	Taken       time.Time    `json:"taken"`
	Source      string       `json:"source"` // which tool or file produced it
	Supported   bool         `json:"supported"`
	Error       string       `json:"error,omitempty"`
	Processes   []Process    `json:"processes"`
	Connections []Connection `json:"connections"`
}
