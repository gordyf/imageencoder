package imagestore

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	"go.etcd.io/bbolt"
)

var (
	tilesBucket    = []byte("tiles")
	deltasBucket   = []byte("deltas")
	imagesBucket   = []byte("images")
	featuresBucket = []byte("features")
)

// BoltImageStore implements ImageStore using BoltDB
type BoltImageStore struct {
	db                *bbolt.DB
	config            *Config
	similarityMatcher *SimilarityMatcher
	encoder           *zstd.Encoder
	decoder           *zstd.Decoder
}

// NewBoltImageStore creates a new BoltDB-backed image store
func NewBoltImageStore(config *Config) (*BoltImageStore, error) {
	// Ensure database directory exists
	dbDir := filepath.Dir(config.DatabasePath)
	if dbDir != "" && dbDir != "." {
		// Create directory if it doesn't exist (simplified)
	}

	db, err := bbolt.Open(config.DatabasePath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		buckets := [][]byte{tilesBucket, deltasBucket, imagesBucket, featuresBucket}
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	// Create zstd encoder and decoder
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		db.Close()
		encoder.Close()
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}

	store := &BoltImageStore{
		db:                db,
		config:            config,
		similarityMatcher: NewSimilarityMatcher(),
		encoder:           encoder,
		decoder:           decoder,
	}

	// Load existing features into similarity matcher
	err = store.loadFeatures()
	if err != nil {
		log.Printf("Warning: failed to load features: %v", err)
	}

	return store, nil
}

// StoreImage stores an image using tile-based deduplication
func (s *BoltImageStore) StoreImage(id string, imageData []byte) error {
	dedupMatch := 0
	directStore := 0
	deltaStore := 0
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
		ID:       id,
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
		TileRefs: make([]TileRef, len(tileRefs)),
		Metadata: make(map[string]string),
	}

	return s.db.Update(func(tx *bbolt.Tx) error {
		tilesBkt := tx.Bucket(tilesBucket)
		deltasBkt := tx.Bucket(deltasBucket)
		imagesBkt := tx.Bucket(imagesBucket)
		featuresBkt := tx.Bucket(featuresBucket)

		fmt.Println("considering ", len(tiles), "tiles for image", id)

		// Process each tile
		for i, tile := range tiles {
			tileKey := []byte(tile.ID)

			// Check if exact tile already exists (by hash)
			if existing := tilesBkt.Get(tileKey); existing != nil {
				dedupMatch++
				// Tile already exists, just reference it
				storedImage.TileRefs[i] = TileRef{
					X:           tileRefs[i].X,
					Y:           tileRefs[i].Y,
					TileID:      tileRefs[i].TileID,
					IsDelta:     false,
					StorageType: StorageDuplicate,
				}
				continue
			}
			if existing := deltasBkt.Get(tileKey); existing != nil {
				dedupMatch++
				storedImage.TileRefs[i] = TileRef{
					X:           tileRefs[i].X,
					Y:           tileRefs[i].Y,
					TileID:      tileRefs[i].TileID,
					IsDelta:     false,
					StorageType: StorageDuplicate,
				}
				continue
			}

			// Check if we have any tiles at all for similarity matching
			if s.similarityMatcher.Size() == 0 {
				directStore++
				// No existing tiles, store this one directly (compressed)
				compressedData, err := s.compressTileData(tile.Data)
				if err != nil {
					return fmt.Errorf("failed to compress tile %s: %w", tile.ID, err)
				}
				err = tilesBkt.Put(tileKey, compressedData)
				if err != nil {
					return fmt.Errorf("failed to store tile %s: %w", tile.ID, err)
				}

				// Store features
				features, err := ExtractTileFeatures(tile.ID, tile.Data, s.config.TileSize)
				if err == nil {
					featuresBytes, err := json.Marshal(features)
					if err == nil {
						featuresBkt.Put(tileKey, featuresBytes)
						s.similarityMatcher.AddTile(tile.ID, tile.Data, s.config.TileSize)
					}
				}

				storedImage.TileRefs[i] = TileRef{
					X:           tileRefs[i].X,
					Y:           tileRefs[i].Y,
					TileID:      tileRefs[i].TileID,
					IsDelta:     false,
					StorageType: StorageUnique,
				}
				continue
			}

			// Find similar tile for delta encoding (only if enabled)
			var bestMatch *TileID
			var err error
			if s.config.EnableDeltaTiles {
				bestMatch, err = s.similarityMatcher.BestMatch(
					tile.Data,
					s.config.TileSize,
					func(tileID TileID) ([]byte, error) {
						return s.getTileDataFromTx(tx, tileID)
					},
				)
			}

			if bestMatch == nil {
				noBestMatch++
			}

			if s.config.EnableDeltaTiles && err == nil && bestMatch != nil {
				// Create delta
				baseData, err := s.getTileDataFromTx(tx, *bestMatch)
				if err == nil {
					deltaData, err := ComputeDelta(tile.Data, baseData, s.config.TileSize)
					if err == nil {
						// Only use delta if it's significantly smaller (at least 25% savings)
						deltaIsSmaller := len(deltaData) < (len(tile.Data) * 3 / 4)
						// debug log if delta is not smaller
						if !deltaIsSmaller {
							log.Printf("Delta for tile %s is not smaller than original (%d vs %d bytes)", tile.ID, len(deltaData), len(tile.Data))
						} else {
							deltaStore++
							deltaKey := []byte(tile.ID)
							tileDelta := CreateTileDelta(*bestMatch, deltaData)

							deltaBytes, err := json.Marshal(tileDelta)
							if err == nil {
								err = deltasBkt.Put(deltaKey, deltaBytes)
								if err == nil {
									storedImage.TileRefs[i] = TileRef{
										X:           tileRefs[i].X,
										Y:           tileRefs[i].Y,
										TileID:      tile.ID,
										IsDelta:     true,
										StorageType: StorageDelta,
									}
									continue
								}
							}
						}
					}
				}
			}
			directStore++
			// Store as new tile (compressed)
			compressedData, err := s.compressTileData(tile.Data)
			if err != nil {
				return fmt.Errorf("failed to compress tile %s: %w", tile.ID, err)
			}
			err = tilesBkt.Put(tileKey, compressedData)
			if err != nil {
				return fmt.Errorf("failed to store tile %s: %w", tile.ID, err)
			}

			// Store features
			features, err := ExtractTileFeatures(tile.ID, tile.Data, s.config.TileSize)
			if err == nil {
				featuresBytes, err := json.Marshal(features)
				if err == nil {
					featuresBkt.Put(tileKey, featuresBytes)
					s.similarityMatcher.AddTile(tile.ID, tile.Data, s.config.TileSize)
				}
			}

			storedImage.TileRefs[i] = TileRef{
				X:           tileRefs[i].X,
				Y:           tileRefs[i].Y,
				TileID:      tileRefs[i].TileID,
				IsDelta:     false,
				StorageType: StorageUnique,
			}
		}

		// Store image metadata
		imageBytes, err := json.Marshal(storedImage)
		if err != nil {
			return fmt.Errorf("failed to marshal image metadata: %w", err)
		}
		fmt.Println("Deduplication matches found:", dedupMatch)
		fmt.Println("Direct stores:", directStore, "Delta stores:", deltaStore)
		fmt.Println("No best matches found:", noBestMatch)
		return imagesBkt.Put([]byte(id), imageBytes)
	})
}

// RetrieveImage reconstructs and returns an image
func (s *BoltImageStore) RetrieveImage(id string) ([]byte, error) {
	var storedImage StoredImage

	err := s.db.View(func(tx *bbolt.Tx) error {
		imagesBkt := tx.Bucket(imagesBucket)
		imageData := imagesBkt.Get([]byte(id))
		if imageData == nil {
			return fmt.Errorf("image not found: %s", id)
		}

		return json.Unmarshal(imageData, &storedImage)
	})
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
func (s *BoltImageStore) DeleteImage(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		imagesBkt := tx.Bucket(imagesBucket)
		imageData := imagesBkt.Get([]byte(id))
		if imageData == nil {
			return fmt.Errorf("image not found: %s", id)
		}

		var storedImage StoredImage
		err := json.Unmarshal(imageData, &storedImage)
		if err != nil {
			return fmt.Errorf("failed to unmarshal image: %w", err)
		}

		// Delete image metadata
		err = imagesBkt.Delete([]byte(id))
		if err != nil {
			return err
		}

		// TODO: Implement reference counting to delete unreferenced tiles
		// For now, we keep tiles to avoid complexity

		return nil
	})
}

// ListImages returns all stored image IDs
func (s *BoltImageStore) ListImages() ([]string, error) {
	var imageIDs []string

	err := s.db.View(func(tx *bbolt.Tx) error {
		imagesBkt := tx.Bucket(imagesBucket)
		return imagesBkt.ForEach(func(k, v []byte) error {
			imageIDs = append(imageIDs, string(k))
			return nil
		})
	})

	return imageIDs, err
}

// GetStorageStats returns storage statistics
func (s *BoltImageStore) GetStorageStats() StorageStats {
	var stats StorageStats

	s.db.View(func(tx *bbolt.Tx) error {
		// Count images
		imagesBkt := tx.Bucket(imagesBucket)
		imagesBkt.ForEach(func(k, v []byte) error {
			stats.TotalImages++
			return nil
		})

		// Count tiles
		tilesBkt := tx.Bucket(tilesBucket)
		tilesBkt.ForEach(func(k, v []byte) error {
			stats.UniqueTiles++
			stats.StorageBytes += int64(len(v))
			return nil
		})

		// Count deltas
		deltasBkt := tx.Bucket(deltasBucket)
		deltasBkt.ForEach(func(k, v []byte) error {
			stats.TotalDeltas++
			stats.StorageBytes += int64(len(v))
			return nil
		})

		return nil
	})

	// Calculate compression ratio (simplified)
	if stats.TotalImages > 0 {
		expectedSize := int64(stats.TotalImages) * int64(s.config.TileSize*s.config.TileSize*3)
		if expectedSize > 0 {
			stats.CompressionRatio = float64(expectedSize) / float64(stats.StorageBytes)
		}
	}

	return stats
}

// Close closes the database
func (s *BoltImageStore) Close() error {
	if s.encoder != nil {
		s.encoder.Close()
	}
	if s.decoder != nil {
		s.decoder.Close()
	}
	return s.db.Close()
}

// compressTileData compresses tile data using zstd
func (s *BoltImageStore) compressTileData(data []byte) ([]byte, error) {
	return s.encoder.EncodeAll(data, make([]byte, 0, len(data))), nil
}

// decompressTileData decompresses tile data using zstd
func (s *BoltImageStore) decompressTileData(compressedData []byte) ([]byte, error) {
	return s.decoder.DecodeAll(compressedData, nil)
}

// RetrieveDebugImage generates a color-coded debug visualization
func (s *BoltImageStore) RetrieveDebugImage(id string) ([]byte, error) {
	var storedImage StoredImage

	err := s.db.View(func(tx *bbolt.Tx) error {
		imagesBkt := tx.Bucket(imagesBucket)
		imageData := imagesBkt.Get([]byte(id))
		if imageData == nil {
			return fmt.Errorf("image not found: %s", id)
		}

		return json.Unmarshal(imageData, &storedImage)
	})
	if err != nil {
		return nil, err
	}

	// Create debug image with color-coded tiles
	img := image.NewRGBA(image.Rect(0, 0, storedImage.Width, storedImage.Height))

	// Define colors for different storage types
	colors := map[StorageType]color.RGBA{
		StorageUnique:    {0, 255, 0, 255},   // Green - newly stored tile
		StorageDuplicate: {0, 0, 255, 255},   // Blue - exact duplicate
		StorageDelta:     {255, 255, 0, 255}, // Yellow - delta encoded
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
func (s *BoltImageStore) getTileData(tileID TileID) ([]byte, error) {
	var data []byte

	err := s.db.View(func(tx *bbolt.Tx) error {
		var err error
		data, err = s.getTileDataFromTx(tx, tileID)
		return err
	})

	return data, err
}

// getTileDataFromTx retrieves tile data within a transaction
func (s *BoltImageStore) getTileDataFromTx(tx *bbolt.Tx, tileID TileID) ([]byte, error) {
	tileKey := []byte(tileID)

	// Try tiles bucket first
	tilesBkt := tx.Bucket(tilesBucket)
	if compressedData := tilesBkt.Get(tileKey); compressedData != nil {
		// Decompress the tile data
		decompressedData, err := s.decompressTileData(compressedData)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress tile %s: %w", tileID, err)
		}
		return decompressedData, nil
	}

	// Try deltas bucket
	deltasBkt := tx.Bucket(deltasBucket)
	if deltaData := deltasBkt.Get(tileKey); deltaData != nil {
		var tileDelta TileDelta
		err := json.Unmarshal(deltaData, &tileDelta)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal delta: %w", err)
		}

		// Get base tile
		baseData, err := s.getTileDataFromTx(tx, tileDelta.BaseID)
		if err != nil {
			return nil, fmt.Errorf("failed to get base tile %s: %w", tileDelta.BaseID, err)
		}

		// Apply delta
		return ApplyDelta(baseData, tileDelta.Delta, s.config.TileSize)
	}

	return nil, fmt.Errorf("tile not found: %s", tileID)
}

// loadFeatures loads existing tile features into the similarity matcher
func (s *BoltImageStore) loadFeatures() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		featuresBkt := tx.Bucket(featuresBucket)

		return featuresBkt.ForEach(func(k, v []byte) error {
			var features TileFeatures
			err := json.Unmarshal(v, &features)
			if err != nil {
				log.Printf("Warning: failed to unmarshal features for tile %s: %v", k, err)
				return nil // Continue with other features
			}

			// Add to similarity matcher (we don't need the actual tile data here)
			s.similarityMatcher.features = append(s.similarityMatcher.features, features)
			return nil
		})
	})
}
