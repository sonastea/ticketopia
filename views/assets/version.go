package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sync"
)

// versions maps each embedded file to a short hash of its bytes. A content hash,
// unlike a commit, keeps an unchanged file's URL (and its caches) across deploys.
var versions = sync.OnceValue(func() map[string]string {
	sums := map[string]string{}
	err := fs.WalkDir(Files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := Files.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		sums[name] = hex.EncodeToString(sum[:6])
		return nil
	})
	if err != nil {
		panic(err) // The embedded tree is fixed at build time.
	}
	return sums
})

// Path returns the public URL of an embedded file, versioned by its content so
// shared and browser caches cannot serve an older copy after a deploy.
func Path(name string) string {
	if v, ok := Version(name); ok {
		return "/assets/" + name + "?v=" + v
	}
	return "/assets/" + name
}

// Version returns the content version Path issues for an embedded file.
func Version(name string) (string, bool) {
	v, ok := versions()[name]
	return v, ok
}
