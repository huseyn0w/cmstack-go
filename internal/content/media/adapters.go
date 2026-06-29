package media

import "github.com/huseyn0w/cmstack-go/internal/platform/storage"

// thumbnailerFunc adapts storage.GenerateThumbnails (with a fixed spec set) to
// the Thumbnailer interface. The wiring uses DefaultThumbnailSpecs; tests may
// inject their own.
type thumbnailerFunc struct {
	specs []storage.ThumbnailSpec
}

// NewThumbnailer returns the default Thumbnailer (DefaultThumbnailSpecs) backed
// by storage.GenerateThumbnails. Pass specs to override.
func NewThumbnailer(specs ...storage.ThumbnailSpec) Thumbnailer {
	if len(specs) == 0 {
		specs = storage.DefaultThumbnailSpecs
	}
	return thumbnailerFunc{specs: specs}
}

// Generate renders the configured variants from the validated image bytes.
func (t thumbnailerFunc) Generate(data []byte, sourceMIME string) ([]storage.GeneratedThumbnail, error) {
	return storage.GenerateThumbnails(data, sourceMIME, t.specs)
}
