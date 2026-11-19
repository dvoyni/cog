package canvas

import (
	"fmt"
	"image"
	"io/fs"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// imageSizeTable is the size of each image file Assets.ImageSize was asked
// of, read from its header alone, by its path, until unloaded. Any task
// holding Assets reads and adds to it, so its mutex guards sizes.
type imageSizeTable struct {
	mutex sync.RWMutex
	sizes map[string]headerSize
}

// find answers the size of the image file at path in files, in texels, and
// whether its header was read: from the table, or else read from the
// header, outside the mutex, so other calls do not wait for the read, and
// kept, the first kept when two calls read it. A header that cannot be
// read is kept as unread, and reported to logger, once for path.
func (t *imageSizeTable) find(files fs.FS, logger *kernel.Logger, path string) (m.Vec2, bool) {
	t.mutex.RLock()
	size, found := t.sizes[path]
	t.mutex.RUnlock()
	if found {
		return size.size, size.read
	}
	config, err := readImageConfig(files, path)
	if err == nil {
		size = headerSize{size: m.Vec2{X: float32(config.Width), Y: float32(config.Height)}, read: true}
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if kept, found := t.sizes[path]; found {
		return kept.size, kept.read
	}
	if err != nil {
		unreadReport := Report{Kind: ReportKindImageHeaderUnread, Err: fmt.Errorf("canvas: reading the header of image %q: %w", path, err)}
		logger.LogErrOnce(imageHeaderUnreadLogKey+path, unreadReport)
	}
	if t.sizes == nil {
		t.sizes = map[string]headerSize{}
	}
	t.sizes[path] = size
	return size.size, size.read
}

// forget forgets the size of each path Unload(unloaded) unloads, so the
// next find reads its header again, and the report logged to logger of its
// header unread, so it may be reported again.
func (t *imageSizeTable) forget(logger *kernel.Logger, unloaded string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for path := range t.sizes {
		if isUnder(path, unloaded) {
			delete(t.sizes, path)
			logger.ForgetLogOnce(imageHeaderUnreadLogKey + path)
		}
	}
}

// headerSize is an image file's size in texels, as its header gives it,
// and whether its header was read.
type headerSize struct {
	size m.Vec2
	read bool
}

// readImageConfig answers the image file at path in files' header: its
// size and colour model, read with image.DecodeConfig, its body unread. An
// image of no texels is an error, as render's loads take it.
func readImageConfig(files fs.FS, path string) (image.Config, error) {
	file, err := files.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err == nil && (config.Width <= 0 || config.Height <= 0) {
		err = fmt.Errorf("an image of %d×%d texels", config.Width, config.Height)
	}
	return config, err
}
