package services

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	svc "github.com/yourusername/trough/services"
)

func TestLocalStorage_Save_Delete(t *testing.T) {
	t.Cleanup(func() { os.RemoveAll("test-uploads") })
	st := svc.NewLocalStorage("test-uploads")

	data := []byte("hello world")
	url, err := st.Save(context.Background(), filepath.ToSlash("avatars/foo.txt"), bytes.NewReader(data), "text/plain")
	assert.NoError(t, err)
	assert.Equal(t, "/uploads/avatars/foo.txt", url)

	// File should exist on disk
	b, err := os.ReadFile(filepath.Join("test-uploads", "avatars", "foo.txt"))
	assert.NoError(t, err)
	assert.Equal(t, data, b)

	// Public URL
	assert.Equal(t, "/uploads/avatars/foo.txt", st.PublicURL("avatars/foo.txt"))

	// Delete
	err = st.Delete(context.Background(), "avatars/foo.txt")
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join("test-uploads", "avatars", "foo.txt"))
	assert.True(t, os.IsNotExist(err))
}

