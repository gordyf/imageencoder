package imagestore

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

func TestMakeKey(t *testing.T) {
	bucket := []byte("test")
	suffix := "key123"
	
	key := makeKey(bucket, suffix)
	expected := "test:key123"
	
	if string(key) != expected {
		t.Errorf("expected key %s, got %s", expected, string(key))
	}
}

func TestMakePrefixKey(t *testing.T) {
	bucket := []byte("images")
	
	prefix := makePrefixKey(bucket)
	expected := "images:"
	
	if string(prefix) != expected {
		t.Errorf("expected prefix %s, got %s", expected, string(prefix))
	}
}

func TestNewPebbleImageStore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := &Config{
		TileSize:            256,
		SimilarityThreshold: 0.05,
		DatabasePath:        dbPath,
	}
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	if store.config.TileSize != 256 {
		t.Errorf("expected tile size 256, got %d", store.config.TileSize)
	}
}

func TestStoreAndRetrieveImage(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := &Config{
		TileSize:            4, // Small tile size for testing
		SimilarityThreshold: 0.05,
		DatabasePath:        dbPath,
	}
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Create a test image (8x8 with distinct colors)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r := uint8((x + y) * 32 % 256)
			g := uint8((x * y) % 256)
			b := uint8((x - y + 8) * 32 % 256)
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	
	// Encode to PNG
	imageData, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	
	// Store the image
	imageID := "test-image-1"
	err = store.StoreImage(imageID, imageData)
	if err != nil {
		t.Fatalf("failed to store image: %v", err)
	}
	
	// Retrieve the image
	retrievedData, err := store.RetrieveImage(imageID)
	if err != nil {
		t.Fatalf("failed to retrieve image: %v", err)
	}
	
	// Decode retrieved image
	retrievedImg, err := decodeImageFromBytes(retrievedData)
	if err != nil {
		t.Fatalf("failed to decode retrieved image: %v", err)
	}
	
	// Verify dimensions
	bounds := retrievedImg.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("expected 8x8 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	
	// Verify pixel values are exactly the same (lossless storage)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			originalR, originalG, originalB, originalA := img.At(x, y).RGBA()
			retrievedR, retrievedG, retrievedB, retrievedA := retrievedImg.At(x, y).RGBA()
			
			// Storage should be lossless - pixels must match exactly
			if originalR != retrievedR || originalG != retrievedG || originalB != retrievedB || originalA != retrievedA {
				t.Errorf("pixel (%d,%d) mismatch: original RGBA(%d,%d,%d,%d), retrieved RGBA(%d,%d,%d,%d)",
					x, y, originalR>>8, originalG>>8, originalB>>8, originalA>>8,
					retrievedR>>8, retrievedG>>8, retrievedB>>8, retrievedA>>8)
			}
		}
	}
}

func TestRetrieveNonExistentImage(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	_, err = store.RetrieveImage("nonexistent")
	if err == nil {
		t.Error("expected error when retrieving nonexistent image")
	}
}

func TestListImages(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Initially should be empty
	images, err := store.ListImages()
	if err != nil {
		t.Fatalf("failed to list images: %v", err)
	}
	
	if len(images) != 0 {
		t.Errorf("expected 0 images initially, got %d", len(images))
	}
	
	// Create and store test images
	img := createTestImage(4, 4)
	imageData, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	
	imageIDs := []string{"image1", "image2", "image3"}
	for _, id := range imageIDs {
		err = store.StoreImage(id, imageData)
		if err != nil {
			t.Fatalf("failed to store image %s: %v", id, err)
		}
	}
	
	// List images again
	images, err = store.ListImages()
	if err != nil {
		t.Fatalf("failed to list images: %v", err)
	}
	
	if len(images) != len(imageIDs) {
		t.Errorf("expected %d images, got %d", len(imageIDs), len(images))
	}
	
	// Verify all image IDs are present
	imageMap := make(map[string]bool)
	for _, id := range images {
		imageMap[id] = true
	}
	
	for _, expectedID := range imageIDs {
		if !imageMap[expectedID] {
			t.Errorf("expected image ID %s not found in list", expectedID)
		}
	}
}

func TestDeleteImage(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Store a test image
	img := createTestImage(4, 4)
	imageData, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	
	imageID := "test-delete"
	err = store.StoreImage(imageID, imageData)
	if err != nil {
		t.Fatalf("failed to store image: %v", err)
	}
	
	// Verify image exists
	_, err = store.RetrieveImage(imageID)
	if err != nil {
		t.Fatalf("image should exist before deletion: %v", err)
	}
	
	// Delete the image
	err = store.DeleteImage(imageID)
	if err != nil {
		t.Fatalf("failed to delete image: %v", err)
	}
	
	// Verify image no longer exists
	_, err = store.RetrieveImage(imageID)
	if err == nil {
		t.Error("image should not exist after deletion")
	}
}

func TestGetStorageStats(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	config.TileSize = 4 // Small tile size for predictable testing
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Initial stats should be zero
	stats := store.GetStorageStats()
	if stats.TotalImages != 0 {
		t.Errorf("expected 0 total images initially, got %d", stats.TotalImages)
	}
	
	// Store a test image
	img := createTestImage(8, 8) // 8x8 will create 2x2 = 4 tiles with 4x4 tile size
	imageData, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	
	err = store.StoreImage("test", imageData)
	if err != nil {
		t.Fatalf("failed to store image: %v", err)
	}
	
	// Check stats after storing image
	stats = store.GetStorageStats()
	if stats.TotalImages != 1 {
		t.Errorf("expected 1 total image, got %d", stats.TotalImages)
	}
	
	if stats.TotalTiles != 4 {
		t.Errorf("expected 4 total tiles (2x2), got %d", stats.TotalTiles)
	}
	
	if stats.OriginalBytes <= 0 {
		t.Errorf("expected positive original bytes, got %d", stats.OriginalBytes)
	}
}

func TestCompressDecompressTileData(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	config.TileSize = 4
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Create test tile data (4x4 RGB)
	tileData := make([]byte, 4*4*3)
	for i := 0; i < len(tileData); i += 3 {
		tileData[i] = uint8(i % 256)     // R
		tileData[i+1] = uint8((i+1) % 256) // G
		tileData[i+2] = uint8((i+2) % 256) // B
	}
	
	// Compress tile data
	compressed, err := store.compressTileData(tileData)
	if err != nil {
		t.Fatalf("failed to compress tile data: %v", err)
	}
	
	// Decompress tile data
	decompressed, err := store.decompressTileData(compressed)
	if err != nil {
		t.Fatalf("failed to decompress tile data: %v", err)
	}
	
	// Verify data matches original
	if len(decompressed) != len(tileData) {
		t.Errorf("decompressed data size mismatch: expected %d, got %d", len(tileData), len(decompressed))
	}
	
	for i := 0; i < len(tileData); i++ {
		if decompressed[i] != tileData[i] {
			t.Errorf("decompressed data mismatch at byte %d: expected %d, got %d", i, tileData[i], decompressed[i])
		}
	}
}

func TestInvalidTileDataCompression(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	
	config := DefaultConfig()
	config.DatabasePath = dbPath
	config.TileSize = 4
	
	store, err := NewPebbleImageStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()
	
	// Try to compress data with wrong size
	invalidData := make([]byte, 10) // Should be 4*4*3 = 48 bytes
	
	_, err = store.compressTileData(invalidData)
	if err == nil {
		t.Error("expected error for invalid tile data size")
	}
}

// Helper functions
func createTestImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8((x + y) * 64 % 256)
			g := uint8((x * y) % 256)
			b := uint8((x - y + width) * 32 % 256)
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}

