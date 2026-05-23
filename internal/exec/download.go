package exec

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"xsh/internal/log"
)

// downloadTimeout caps a single Download call. Large .deb fetches (~50MB) over
// a slow link must still fit comfortably; bumping past 10min would mask real
// network outages, so we keep it tight.
const downloadTimeout = 10 * time.Minute

// Download fetches url and writes the body to dst. If dst already exists with a
// non-zero size the call is a no-op (idempotent), matching the rest of the
// install pipeline's "skip if already done" semantics. No checksum verification
// is performed — callers that need integrity (e.g. cri-dockerd) currently
// upstream-publish no sha256 file, so adding a knob here would be dead code.
func Download(url, dst string) error {
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		log.Info("download: %s already present, skipping", dst)
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dst, err)
	}

	log.Info("[DL] %s -> %s", url, dst)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http get %s: status %s", url, resp.Status)
	}

	// Write to a temp file in the same dir then rename so a partial download
	// never leaves a half-written file under dst (the next idempotent run would
	// happily skip a 0.5MB stub).
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".part-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// On the happy path tmp is already closed and renamed; this just covers
		// the error paths.
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, dst, err)
	}
	return nil
}
