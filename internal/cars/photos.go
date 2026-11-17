package cars

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

const (
	// MaxPhotosPerCar caps how many photos one car can have.
	MaxPhotosPerCar = 10
	// MaxPhotoBytes is the largest accepted upload.
	MaxPhotoBytes = 5 << 20

	photoURLTTL = time.Hour
)

// PhotoStore is the object storage the photos live in.
type PhotoStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Photos turns stored photo keys into downloadable links.
type Photos struct{ store PhotoStore }

// NewPhotos wraps a store. A nil store is allowed and yields empty links.
func NewPhotos(store PhotoStore) *Photos { return &Photos{store: store} }

// Resolve returns a temporary download URL for each key.
func (p *Photos) Resolve(ctx context.Context, keys []string) []Photo {
	out := make([]Photo, 0, len(keys))
	for _, k := range keys {
		url := ""
		if p.store != nil {
			if u, err := p.store.PresignGet(ctx, k, photoURLTTL); err == nil {
				url = u
			}
		}
		out = append(out, Photo{Key: k, URL: url})
	}
	return out
}

// Enabled reports whether a store is configured.
func (p *Photos) Enabled() bool { return p.store != nil }

var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// UploadPhoto stores an image for the car. The content type is detected from
// the bytes themselves, never trusted from the client.
func (s *Service) UploadPhoto(ctx context.Context, id uuid.UUID, data []byte) (*Car, error) {
	if !s.photos.Enabled() {
		return nil, httpx.ServiceUnavailable("photo storage is not configured")
	}
	if len(data) == 0 {
		return nil, httpx.BadRequest("the photo file is empty")
	}
	if len(data) > MaxPhotoBytes {
		return nil, httpx.PayloadTooLarge()
	}
	contentType := http.DetectContentType(data)
	ext, ok := imageTypes[contentType]
	if !ok {
		return nil, httpx.Validation([]httpx.FieldError{{Field: "file", Message: "must be a JPEG, PNG or WebP image"}})
	}

	car, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(car.PhotoKeys) >= MaxPhotosPerCar {
		return nil, httpx.Conflict("a car can have at most 10 photos")
	}

	key := "cars/" + id.String() + "/" + uuid.NewString() + ext
	if err := s.photos.store.Put(ctx, key, contentType, data); err != nil {
		return nil, err
	}
	added, err := s.repo.AddPhoto(ctx, id, key, MaxPhotosPerCar)
	if err != nil || !added {
		_ = s.photos.store.Delete(context.WithoutCancel(ctx), key) // do not leave an orphan
		if err != nil {
			return nil, err
		}
		return nil, httpx.Conflict("a car can have at most 10 photos")
	}
	return s.Get(ctx, id)
}

// DeletePhoto removes one of the car's photos. Only keys that belong to the car
// can be deleted, so this can never touch other objects in the bucket.
func (s *Service) DeletePhoto(ctx context.Context, id uuid.UUID, key string) (*Car, error) {
	car, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	found := false
	for _, k := range car.PhotoKeys {
		if k == key {
			found = true
			break
		}
	}
	if !found {
		return nil, httpx.NotFound("photo")
	}
	if err := s.repo.RemovePhoto(ctx, id, key); err != nil {
		return nil, err
	}
	if s.photos.Enabled() {
		_ = s.photos.store.Delete(context.WithoutCancel(ctx), key) // best effort
	}
	return s.Get(ctx, id)
}
