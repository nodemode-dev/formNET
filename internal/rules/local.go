package rules

import (
	"errors"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrLocalMissing: the mapped file does not exist; the answer is 404.
	ErrLocalMissing = errors.New("no such file under the mapped folder")
	// ErrLocalOutside: the path would leave the mapped folder or reach FormNET's data folder; the answer is 403.
	ErrLocalOutside = errors.New("the path leaves the mapped folder")
)

// LocalFile is the file a map_local rule answers a request with.
type LocalFile struct {
	Path        string
	ContentType string
	Size        int64
	Info        os.FileInfo // the opener checks the file it opened is this one
}

// ResolveLocal finds the file c answers reqPath (the decoded URL path) with.
// A file answers every match. A folder maps the part of the path after the
// match's literal path prefix under it, confined to the folder: a .. segment,
// a symlink that resolves outside, or anything in FormNET's data folder is
// refused; a part that is empty or ends in / or names a folder serves its
// index.html.
func (e *Engine) ResolveLocal(c *Compiled, reqPath string) (LocalFile, error) {
	root := c.Action.LocalPath
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return LocalFile{}, ErrLocalMissing
	}
	if e.UnderData(realRoot) {
		return LocalFile{}, ErrLocalOutside
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return LocalFile{}, ErrLocalMissing
	}
	if !info.IsDir() {
		return e.localFile(realRoot, realRoot)
	}
	rest := strings.TrimPrefix(reqPath, c.Matcher.PathPrefix())
	if strings.ContainsAny(rest, "\x00\\") {
		return LocalFile{}, ErrLocalOutside
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == ".." {
			return LocalFile{}, ErrLocalOutside
		}
	}
	if rest == "" || strings.HasSuffix(rest, "/") {
		rest += "index.html"
	}
	joined := filepath.Join(realRoot, filepath.FromSlash(strings.TrimPrefix(rest, "/")))
	if !Within(joined, realRoot) {
		return LocalFile{}, ErrLocalOutside
	}
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return LocalFile{}, ErrLocalMissing
	}
	if st, err := os.Stat(real); err == nil && st.IsDir() {
		joined = filepath.Join(joined, "index.html")
		if real, err = filepath.EvalSymlinks(joined); err != nil {
			return LocalFile{}, ErrLocalMissing
		}
	}
	return e.localFile(real, realRoot)
}

func (e *Engine) localFile(real, realRoot string) (LocalFile, error) {
	if !Within(real, realRoot) || e.UnderData(real) {
		return LocalFile{}, ErrLocalOutside
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return LocalFile{}, ErrLocalMissing
	}
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(real)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return LocalFile{Path: real, ContentType: ct, Size: st.Size(), Info: st}, nil
}
