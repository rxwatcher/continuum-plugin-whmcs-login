package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
)

// fakeFS is a tiny in-memory http.FileSystem used by SPA tests so we don't
// need a real web/dist to exercise the handler.
type fakeFS map[string][]byte

func (f fakeFS) Open(name string) (http.File, error) {
	data, ok := f[name]
	if !ok {
		// http.FileSystem callers sometimes drop the leading slash.
		data, ok = f["/"+strings.TrimPrefix(name, "/")]
	}
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &fakeHTTPFile{name: name, Reader: bytes.NewReader(data), size: int64(len(data))}, nil
}

type fakeHTTPFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *fakeHTTPFile) Close() error                       { return nil }
func (f *fakeHTTPFile) Readdir(_ int) ([]os.FileInfo, error) { return nil, nil }
func (f *fakeHTTPFile) Stat() (os.FileInfo, error)         { return f, nil }

func (f *fakeHTTPFile) Name() string       { return f.name }
func (f *fakeHTTPFile) Size() int64        { return f.size }
func (f *fakeHTTPFile) Mode() os.FileMode  { return 0o644 }
func (f *fakeHTTPFile) ModTime() time.Time { return time.Time{} }
func (f *fakeHTTPFile) IsDir() bool        { return false }
func (f *fakeHTTPFile) Sys() any           { return nil }
