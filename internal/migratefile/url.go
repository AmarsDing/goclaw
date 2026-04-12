// Package migratefile builds file:// URLs for golang-migrate's file source driver.
package migratefile

import "path/filepath"

// FileURL returns a file:// URL for a migrations directory.
//
// golang-migrate parses the URL with net/url, then uses os.DirFS on the result.
// Windows paths need a form that parses to "D:/path/..." (Host "D:" + Path "/path/..."),
// i.e. file://D:/path — not file:///D:/path, which becomes Path "/D:/..." and breaks DirFS.
// Unix paths use file:///absolute/path (file:// + "/abs").
func FileURL(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return "file://" + filepath.ToSlash(abs)
}
