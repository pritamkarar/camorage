package storage

import (
	"context"
	"io"
)

// Service is what the web API uses of this package: web.Cloud.
type Service struct {
	Rclone   Rclone
	Google   Google
	Uploader *Uploader
}

func (s Service) Check(ctx context.Context, remote string) error { return s.Rclone.Check(ctx, remote) }
func (s Service) SetSection(name string, kv map[string]string) error {
	return SetSection(s.Rclone.Config, name, kv)
}
func (s Service) RemoveSection(name string) error       { return RemoveSection(s.Rclone.Config, name) }
func (s Service) AuthURL(clientID, state string) string { return s.Google.AuthURL(clientID, state) }
func (s Service) Exchange(ctx context.Context, clientID, secret, code string) (string, error) {
	return s.Google.Exchange(ctx, clientID, secret, code)
}
func (s Service) Uploads() map[string]UploadStatus { return s.Uploader.Status() }
func (s Service) List(ctx context.Context, dir string) ([]File, error) {
	return s.Rclone.List(ctx, dir)
}
func (s Service) Cat(ctx context.Context, path string, offset, count int64) (io.ReadCloser, error) {
	return s.Rclone.Cat(ctx, path, offset, count)
}
