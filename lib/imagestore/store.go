package imagestore

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
)

type TileHash [32]byte

func (h TileHash) String() string {
	return fmt.Sprintf("%x", h[:])
}

type TileID string

type Tile struct {
	ID   TileID
	Hash TileHash
	Data []byte // Raw RGB data for 256x256 tile (256*256*3 bytes)
}

type TileDelta struct {
	BaseID TileID
	Delta  []byte // Compressed difference data
}

type StoredImage struct {
	ID       string
	Width    int
	Height   int
	TileRefs []TileRef
	Metadata map[string]string
}

type TileRef struct {
	X, Y    int    // Position in image (tile coordinates)
	TileID  TileID // Reference to tile or delta
	IsDelta bool   // Whether this references a delta
}

type StorageStats struct {
	TotalImages      int
	UniqueTiles      int
	TotalDeltas      int
	StorageBytes     int64
	CompressionRatio float64
}

type ImageStore interface {
	StoreImage(id string, imageData []byte) error
	RetrieveImage(id string) ([]byte, error)
	DeleteImage(id string) error
	ListImages() ([]string, error)
	GetStorageStats() StorageStats
	Close() error
}

type Config struct {
	TileSize            int     // Default 256
	SimilarityThreshold float64 // Default 0.1 (10% difference threshold)
	DatabasePath        string
}

func DefaultConfig() *Config {
	return &Config{
		TileSize:            256,
		SimilarityThreshold: 0.1,
		DatabasePath:        "./imagestore.db",
	}
}

// ComputeTileHash computes SHA-256 hash of tile data
func ComputeTileHash(data []byte) TileHash {
	return sha256.Sum256(data)
}

// GenerateTileID generates a unique tile ID from hash
func GenerateTileID(hash TileHash) TileID {
	return TileID(hash.String())
}

// decodeImageFromBytes decodes image data from bytes, supporting PNG and JPEG
func decodeImageFromBytes(data []byte) (image.Image, error) {
	reader := bytes.NewReader(data)

	// Try to decode as PNG first
	reader.Seek(0, 0)
	img, err := png.Decode(reader)
	if err == nil {
		return img, nil
	}

	// Try to decode as JPEG
	reader.Seek(0, 0)
	img, err = jpeg.Decode(reader)
	if err == nil {
		return img, nil
	}

	// Try generic image decode
	reader.Seek(0, 0)
	img, _, err = image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// encodeImageToPNG encodes an image to PNG format
func encodeImageToPNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer

	err := png.Encode(&buf, img)
	if err != nil {
		return nil, fmt.Errorf("failed to encode image to PNG: %w", err)
	}

	return buf.Bytes(), nil
}
