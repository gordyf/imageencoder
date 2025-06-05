package imagestore

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"path/filepath"

	"github.com/DataDog/zstd"
	"github.com/cockroachdb/pebble"
)

var (
	tilesBucket  = []byte("tiles")
	imagesBucket = []byte("images")
)

// makeKey safely constructs a key with bucket prefix and suffix
func makeKey(bucket []byte, suffix string) []byte {
	key := make([]byte, 0, len(bucket)+1+len(suffix))
	key = append(key, bucket...)
	key = append(key, ':')
	key = append(key, []byte(suffix)...)
	return key
}

// makePrefixKey safely constructs a prefix key for iteration
func makePrefixKey(bucket []byte) []byte {
	key := make([]byte, 0, len(bucket)+1)
	key = append(key, bucket...)
	key = append(key, ':')
	return key
}

// PebbleImageStore implements ImageStore using Pebble
type PebbleImageStore struct {
	db     *pebble.DB
	config *Config
}

// NewPebbleImageStore creates a new Pebble-backed image store
func NewPebbleImageStore(config *Config) (*PebbleImageStore, error) {
	// Ensure database directory exists
	dbDir := filepath.Dir(config.DatabasePath)
	if dbDir != "" && dbDir != "." {
		// Create directory if it doesn't exist (simplified)
	}

	db, err := pebble.Open(config.DatabasePath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &PebbleImageStore{
		db:     db,
		config: config,
	}

	return store, nil
}

// StoreImage stores an image using tile-based deduplication
func (s *PebbleImageStore) StoreImage(id string, imageData []byte) error {
	dedupMatch := 0
	directStore := 0
	noBestMatch := 0

	// Convert image data to image.Image
	img, err := decodeImageFromBytes(imageData)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	// Extract tiles
	tiles, tileRefs, err := ExtractTiles(img, s.config.TileSize)
	if err != nil {
		return fmt.Errorf("failed to extract tiles: %w", err)
	}

	bounds := img.Bounds()
	storedImage := &StoredImage{
		ID:            id,
		Width:         bounds.Dx(),
		Height:        bounds.Dy(),
		TileRefs:      make([]TileRef, len(tileRefs)),
		Metadata:      make(map[string]string),
		OriginalBytes: int64(len(imageData)), // Store original PNG input size
	}

	// Use batch for atomic operations
	batch := s.db.NewBatch()
	defer batch.Close()

	fmt.Println("considering ", len(tiles), "tiles for image", id)

	// Track tiles we've already processed in this batch for intra-image deduplication
	processedTiles := make(map[TileID]bool)

	// Process each tile
	for i, tile := range tiles {
		tileKey := makeKey(tilesBucket, string(tile.ID))

		// Check if exact tile already exists (by hash)
		if _, closer, err := s.db.Get(tileKey); err == nil {
			closer.Close()
			dedupMatch++
			// Tile already exists, just reference it
			storedImage.TileRefs[i] = TileRef{
				X:           tileRefs[i].X,
				Y:           tileRefs[i].Y,
				TileID:      tileRefs[i].TileID,
				StorageType: StorageDuplicate,
			}
			continue
		}

		// Check if we've already processed this tile in this batch (intra-image deduplication)
		if processedTiles[tile.ID] {
			dedupMatch++
			// Tile already processed in this batch, just reference it
			storedImage.TileRefs[i] = TileRef{
				X:           tileRefs[i].X,
				Y:           tileRefs[i].Y,
				TileID:      tileRefs[i].TileID,
				StorageType: StorageDuplicate,
			}
			continue
		}

		// Mark this tile as processed in this batch
		processedTiles[tile.ID] = true

		directStore++
		// Store as new tile (compressed)
		compressedData, err := s.compressTileData(tile.Data)
		if err != nil {
			return fmt.Errorf("failed to compress tile %s: %w", tile.ID, err)
		}
		err = batch.Set(tileKey, compressedData, pebble.Sync)
		if err != nil {
			return fmt.Errorf("failed to store tile %s: %w", tile.ID, err)
		}

		storedImage.TileRefs[i] = TileRef{
			X:           tileRefs[i].X,
			Y:           tileRefs[i].Y,
			TileID:      tileRefs[i].TileID,
			StorageType: StorageUnique,
		}
	}

	// Store image metadata
	imageBytes, err := json.Marshal(storedImage)
	if err != nil {
		return fmt.Errorf("failed to marshal image metadata: %w", err)
	}
	imageKey := makeKey(imagesBucket, id)
	err = batch.Set(imageKey, imageBytes, pebble.Sync)
	if err != nil {
		return fmt.Errorf("failed to store image metadata: %w", err)
	}

	// Commit the batch
	err = batch.Commit(pebble.Sync)
	if err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	fmt.Println("Deduplication matches found:", dedupMatch)
	fmt.Println("No best matches found:", noBestMatch)
	return nil
}

// RetrieveImage reconstructs and returns an image
func (s *PebbleImageStore) RetrieveImage(id string) ([]byte, error) {
	var storedImage StoredImage

	imageKey := makeKey(imagesBucket, id)
	imageData, closer, err := s.db.Get(imageKey)
	if err != nil {
		return nil, fmt.Errorf("image not found: %s", id)
	}
	defer closer.Close()

	err = json.Unmarshal(imageData, &storedImage)
	if err != nil {
		return nil, err
	}

	// Reconstruct image
	img, err := ReconstructImage(&storedImage, s.config.TileSize, func(tileID TileID) ([]byte, error) {
		return s.getTileData(tileID)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct image: %w", err)
	}

	// Encode to PNG
	return encodeImageToPNG(img)
}

// DeleteImage removes an image and unreferenced tiles
func (s *PebbleImageStore) DeleteImage(id string) error {
	imageKey := makeKey(imagesBucket, id)
	imageData, closer, err := s.db.Get(imageKey)
	if err != nil {
		return fmt.Errorf("image not found: %s", id)
	}
	defer closer.Close()

	var storedImage StoredImage
	err = json.Unmarshal(imageData, &storedImage)
	if err != nil {
		return fmt.Errorf("failed to unmarshal image: %w", err)
	}

	// Delete image metadata
	err = s.db.Delete(imageKey, pebble.Sync)
	if err != nil {
		return err
	}

	// TODO: Implement reference counting to delete unreferenced tiles
	// For now, we keep tiles to avoid complexity

	return nil
}

// ListImages returns all stored image IDs
func (s *PebbleImageStore) ListImages() ([]string, error) {
	var imageIDs []string

	// Create iterator for images bucket
	prefix := makePrefixKey(imagesBucket)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xFF),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		// Extract ID from key (remove bucket prefix and ":")
		id := string(key[len(prefix):])
		imageIDs = append(imageIDs, id)
	}

	return imageIDs, iter.Error()
}

// GetStorageStats returns storage statistics
func (s *PebbleImageStore) GetStorageStats() StorageStats {
	var stats StorageStats

	// Count images and analyze tile usage patterns
	imagesPrefix := makePrefixKey(imagesBucket)
	imagesIter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: imagesPrefix,
		UpperBound: append(imagesPrefix, 0xFF),
	})
	if err != nil {
		return stats
	}
	defer imagesIter.Close()

	for imagesIter.First(); imagesIter.Valid(); imagesIter.Next() {
		stats.TotalImages++

		var storedImage StoredImage
		err := json.Unmarshal(imagesIter.Value(), &storedImage)
		if err == nil {
			// Count tiles by storage type
			for _, tileRef := range storedImage.TileRefs {
				stats.TotalTiles++
				switch tileRef.StorageType {
				case StorageUnique:
					stats.DirectTiles++
				case StorageDuplicate:
					stats.DeduplicatedTiles++
				}
			}

			// Use stored original PNG input size
			stats.OriginalBytes += storedImage.OriginalBytes
		}
	}

	// Count unique tiles and their storage size
	tilesPrefix := makePrefixKey(tilesBucket)
	tilesIter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: tilesPrefix,
		UpperBound: append(tilesPrefix, 0xFF),
	})
	if err == nil {
		defer tilesIter.Close()
		for tilesIter.First(); tilesIter.Valid(); tilesIter.Next() {
			stats.UniqueTiles++
			stats.StorageBytes += int64(len(tilesIter.Value()))
		}
	}

	// Calculate percentages
	if stats.TotalTiles > 0 {
		stats.DirectPercent = float64(stats.DirectTiles) / float64(stats.TotalTiles) * 100.0
		stats.DeduplicatedPercent = float64(stats.DeduplicatedTiles) / float64(stats.TotalTiles) * 100.0
	}

	// Calculate compression ratio based on actual original size vs storage size
	if stats.OriginalBytes > 0 && stats.StorageBytes > 0 {
		stats.CompressionRatio = float64(stats.OriginalBytes) / float64(stats.StorageBytes)
	}

	return stats
}

// Close closes the database
func (s *PebbleImageStore) Close() error {
	return s.db.Close()
}

// compressTileData compresses tile data using zstd
func (s *PebbleImageStore) compressTileData(data []byte) ([]byte, error) {
	expectedSize := s.config.TileSize * s.config.TileSize * 3
	if len(data) != expectedSize {
		return nil, fmt.Errorf("invalid tile data size: expected %d, got %d", expectedSize, len(data))
	}

	// Compress using zstd
	return zstd.Compress(nil, data)
}

// decompressTileData decompresses tile data from zstd
func (s *PebbleImageStore) decompressTileData(compressedData []byte) ([]byte, error) {
	// Decompress using zstd
	data, err := zstd.Decompress(nil, compressedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress zstd tile: %w", err)
	}

	// Validate tile data size
	expectedSize := s.config.TileSize * s.config.TileSize * 3
	if len(data) != expectedSize {
		return nil, fmt.Errorf("invalid decompressed tile data size: expected %d, got %d", expectedSize, len(data))
	}

	return data, nil
}

// RetrieveDebugImage generates a color-coded debug visualization
func (s *PebbleImageStore) RetrieveDebugImage(id string) ([]byte, error) {
	var storedImage StoredImage

	imageKey := makeKey(imagesBucket, id)
	imageData, closer, err := s.db.Get(imageKey)
	if err != nil {
		return nil, fmt.Errorf("image not found: %s", id)
	}
	defer closer.Close()

	err = json.Unmarshal(imageData, &storedImage)
	if err != nil {
		return nil, err
	}

	// Create debug image with color-coded tiles
	img := image.NewRGBA(image.Rect(0, 0, storedImage.Width, storedImage.Height))

	// Define colors for different storage types
	colors := map[StorageType]color.RGBA{
		StorageUnique:    {0, 255, 0, 255}, // Green - newly stored tile
		StorageDuplicate: {0, 0, 255, 255}, // Blue - exact duplicate
	}

	// Fill each tile area with the appropriate color
	for _, tileRef := range storedImage.TileRefs {
		tileColor, ok := colors[tileRef.StorageType]
		if !ok {
			tileColor = color.RGBA{255, 0, 0, 255} // Red for unknown/error
		}

		// Calculate tile boundaries
		startX := tileRef.X * s.config.TileSize
		startY := tileRef.Y * s.config.TileSize
		endX := min(startX+s.config.TileSize, storedImage.Width)
		endY := min(startY+s.config.TileSize, storedImage.Height)

		// Fill tile area with color
		for y := startY; y < endY; y++ {
			for x := startX; x < endX; x++ {
				img.Set(x, y, tileColor)
			}
		}

		// Add a thin border for tile boundaries
		borderColor := color.RGBA{0, 0, 0, 255} // Black border

		// Top and bottom borders
		for x := startX; x < endX; x++ {
			if startY < storedImage.Height {
				img.Set(x, startY, borderColor)
			}
			if endY-1 < storedImage.Height {
				img.Set(x, endY-1, borderColor)
			}
		}

		// Left and right borders
		for y := startY; y < endY; y++ {
			if startX < storedImage.Width {
				img.Set(startX, y, borderColor)
			}
			if endX-1 < storedImage.Width {
				img.Set(endX-1, y, borderColor)
			}
		}
	}

	// Encode to PNG
	return encodeImageToPNG(img)
}

// getTileData retrieves tile data by ID
func (s *PebbleImageStore) getTileData(tileID TileID) ([]byte, error) {
	tileKey := makeKey(tilesBucket, string(tileID))

	// Try tiles bucket first
	if compressedData, closer, err := s.db.Get(tileKey); err == nil {
		defer closer.Close()
		// Decompress the tile data
		decompressedData, err := s.decompressTileData(compressedData)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress tile %s: %w", tileID, err)
		}
		return decompressedData, nil
	}

	return nil, fmt.Errorf("tile not found: %s", tileID)
}
