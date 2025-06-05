package imagestore

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"testing"
)

func TestTileHash(t *testing.T) {
	data := []byte("test data")
	hash := ComputeTileHash(data)
	
	expected := sha256.Sum256(data)
	if hash != expected {
		t.Errorf("hash mismatch: expected %x, got %x", expected, hash)
	}
}

func TestTileHashString(t *testing.T) {
	data := []byte("test")
	hash := ComputeTileHash(data)
	str := hash.String()
	
	if len(str) != 64 { // SHA-256 produces 64 hex characters
		t.Errorf("expected hash string length 64, got %d", len(str))
	}
}

func TestGenerateTileID(t *testing.T) {
	hash := ComputeTileHash([]byte("test"))
	tileID := GenerateTileID(hash)
	
	expected := TileID(hash.String())
	if tileID != expected {
		t.Errorf("tile ID mismatch: expected %s, got %s", expected, tileID)
	}
}

func TestStorageTypeString(t *testing.T) {
	tests := []struct {
		storageType StorageType
		expected    string
	}{
		{StorageUnique, "unique"},
		{StorageDuplicate, "duplicate"},
		{StorageType(99), "unknown"},
	}

	for _, tt := range tests {
		if result := tt.storageType.String(); result != tt.expected {
			t.Errorf("StorageType.String() = %s, expected %s", result, tt.expected)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	if config.TileSize != 256 {
		t.Errorf("expected default tile size 256, got %d", config.TileSize)
	}
	
	if config.SimilarityThreshold != 0.05 {
		t.Errorf("expected default similarity threshold 0.05, got %f", config.SimilarityThreshold)
	}
	
	if config.DatabasePath != "./imagestore.db" {
		t.Errorf("expected default database path './imagestore.db', got %s", config.DatabasePath)
	}
}

func TestDecodeImageFromBytes(t *testing.T) {
	// Create a simple 2x2 RGBA image
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})    // Red
	img.Set(1, 0, color.RGBA{0, 255, 0, 255})    // Green
	img.Set(0, 1, color.RGBA{0, 0, 255, 255})    // Blue
	img.Set(1, 1, color.RGBA{255, 255, 0, 255})  // Yellow

	// Encode to PNG
	pngData, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode image to PNG: %v", err)
	}

	// Test decoding
	decoded, err := decodeImageFromBytes(pngData)
	if err != nil {
		t.Fatalf("failed to decode image: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Errorf("expected 2x2 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Check some pixel colors
	r, g, b, a := decoded.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
		t.Errorf("pixel (0,0) color mismatch: got RGBA(%d,%d,%d,%d)", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestDecodeImageFromBytesInvalidData(t *testing.T) {
	invalidData := []byte("not an image")
	
	_, err := decodeImageFromBytes(invalidData)
	if err == nil {
		t.Error("expected error for invalid image data, got nil")
	}
}

func TestEncodeImageToPNG(t *testing.T) {
	// Create a simple image
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 25), uint8(y * 25), 128, 255})
		}
	}

	// Encode to PNG
	data, err := encodeImageToPNG(img)
	if err != nil {
		t.Fatalf("failed to encode image: %v", err)
	}

	// Check PNG signature
	if len(data) < 8 {
		t.Fatal("PNG data too short")
	}
	
	pngSignature := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.Equal(data[:8], pngSignature) {
		t.Error("invalid PNG signature")
	}

	// Verify we can decode it back
	decoded, err := decodeImageFromBytes(data)
	if err != nil {
		t.Fatalf("failed to decode encoded PNG: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 10 {
		t.Errorf("expected 10x10 image after round-trip, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}