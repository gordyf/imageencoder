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

type StoredImage struct {
	ID            string
	Width         int
	Height        int
	TileRefs      []TileRef
	Metadata      map[string]string
	OriginalBytes int64 // Size of original PNG input data
}

type StorageType uint8

const (
	StorageUnique    StorageType = iota // Newly stored unique tile
	StorageDuplicate                    // Exact duplicate of existing tile
)

func (s StorageType) String() string {
	switch s {
	case StorageUnique:
		return "unique"
	case StorageDuplicate:
		return "duplicate"
	default:
		return "unknown"
	}
}

type TileRef struct {
	X, Y        int         // Position in image (tile coordinates)
	TileID      TileID      // Reference to tile
	StorageType StorageType // How this tile was stored
}

type StorageStats struct {
	TotalImages         int
	TotalTiles          int
	UniqueTiles         int
	DirectTiles         int
	DeduplicatedTiles   int
	DirectPercent       float64
	DeduplicatedPercent float64
	StorageBytes        int64
	OriginalBytes       int64
	CompressionRatio    float64
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
	TileDumpDir         string  // Optional: directory to dump uncompressed tiles for zstd dictionary training
	DictPath            string  // Optional: path to zstd dictionary file for compression
}

func DefaultConfig() *Config {
	return &Config{
		TileSize:            256,
		SimilarityThreshold: 0.05, // More conservative: 5% difference threshold
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
