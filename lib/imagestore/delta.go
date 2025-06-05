package imagestore

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// ComputeDelta calculates the difference between two tiles
func ComputeDelta(newTile, baseTile []byte, tileSize int) ([]byte, error) {
	if len(newTile) != len(baseTile) {
		return nil, fmt.Errorf("tile sizes don't match: %d vs %d", len(newTile), len(baseTile))
	}

	expectedSize := tileSize * tileSize * 3
	if len(newTile) != expectedSize {
		return nil, fmt.Errorf("invalid tile size: expected %d, got %d", expectedSize, len(newTile))
	}

	// Calculate pixel differences
	delta := make([]int8, len(newTile))
	for i := 0; i < len(newTile); i++ {
		diff := int(newTile[i]) - int(baseTile[i])
		// Clamp to int8 range [-128, 127]
		if diff > 127 {
			diff = 127
		} else if diff < -128 {
			diff = -128
		}
		delta[i] = int8(diff)
	}

	// Compress delta data
	compressed, err := compressDelta(delta)
	if err != nil {
		return nil, fmt.Errorf("failed to compress delta: %w", err)
	}

	return compressed, nil
}

// ApplyDelta reconstructs a tile by applying delta to base tile
func ApplyDelta(baseTile, deltaData []byte, tileSize int) ([]byte, error) {
	expectedSize := tileSize * tileSize * 3
	if len(baseTile) != expectedSize {
		return nil, fmt.Errorf("invalid base tile size: expected %d, got %d", expectedSize, len(baseTile))
	}

	// Decompress delta
	delta, err := decompressDelta(deltaData)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress delta: %w", err)
	}

	if len(delta) != expectedSize {
		return nil, fmt.Errorf("delta size mismatch: expected %d, got %d", expectedSize, len(delta))
	}

	// Apply delta to base tile
	result := make([]byte, expectedSize)
	for i := 0; i < expectedSize; i++ {
		value := int(baseTile[i]) + int(delta[i])
		// Clamp to byte range [0, 255]
		if value > 255 {
			value = 255
		} else if value < 0 {
			value = 0
		}
		result[i] = byte(value)
	}

	return result, nil
}

// ComputePerceptualDistance calculates perceptual distance between two tiles
func ComputePerceptualDistance(tile1, tile2 []byte, tileSize int) (float64, error) {
	if len(tile1) != len(tile2) {
		return 0, fmt.Errorf("tile sizes don't match: %d vs %d", len(tile1), len(tile2))
	}

	expectedSize := tileSize * tileSize * 3
	if len(tile1) != expectedSize {
		return 0, fmt.Errorf("invalid tile size: expected %d, got %d", expectedSize, len(tile1))
	}

	// Calculate sum of squared differences
	var sumSquaredDiff float64
	for i := 0; i < len(tile1); i++ {
		diff := float64(tile1[i]) - float64(tile2[i])
		sumSquaredDiff += diff * diff
	}

	// Normalize by number of pixels and max possible difference
	numPixels := float64(tileSize * tileSize)
	maxDiff := 255.0 * 255.0 * 3.0 // 3 channels, max diff per channel is 255

	// Return normalized distance [0, 1]
	return math.Sqrt(sumSquaredDiff / (numPixels * maxDiff)), nil
}

// IsSimilarEnough checks if two tiles are similar enough to warrant delta encoding
func IsSimilarEnough(tile1, tile2 []byte, tileSize int, threshold float64) (bool, float64, error) {
	distance, err := ComputePerceptualDistance(tile1, tile2, tileSize)
	if err != nil {
		return false, 0, err
	}

	return distance <= threshold, distance, nil
}

// EstimateDeltaSize estimates the compressed size of a delta
func EstimateDeltaSize(newTile, baseTile []byte, tileSize int) (int, error) {
	deltaData, err := ComputeDelta(newTile, baseTile, tileSize)
	if err != nil {
		return 0, err
	}
	return len(deltaData), nil
}

// compressDelta compresses delta data using gzip
func compressDelta(delta []int8) ([]byte, error) {
	var buf bytes.Buffer

	// Write header with length
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(delta))); err != nil {
		return nil, err
	}

	// Compress data
	gzWriter := gzip.NewWriter(&buf)

	// Convert int8 to bytes for writing
	deltaBytes := make([]byte, len(delta))
	for i, d := range delta {
		deltaBytes[i] = byte(d)
	}

	if _, err := gzWriter.Write(deltaBytes); err != nil {
		gzWriter.Close()
		return nil, err
	}

	if err := gzWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decompressDelta decompresses delta data
func decompressDelta(compressed []byte) ([]int8, error) {
	buf := bytes.NewReader(compressed)

	// Read header
	var length uint32
	if err := binary.Read(buf, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	// Decompress data
	gzReader, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	deltaBytes, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}

	if len(deltaBytes) != int(length) {
		return nil, fmt.Errorf("decompressed size mismatch: expected %d, got %d", length, len(deltaBytes))
	}

	// Convert bytes back to int8
	delta := make([]int8, len(deltaBytes))
	for i, b := range deltaBytes {
		delta[i] = int8(b)
	}

	return delta, nil
}

// CreateTileDelta creates a TileDelta structure
func CreateTileDelta(baseID TileID, deltaData []byte) *TileDelta {
	return &TileDelta{
		BaseID: baseID,
		Delta:  deltaData,
	}
}

// ValidateDeltaData validates that delta data can be decompressed
func ValidateDeltaData(deltaData []byte, expectedSize int) error {
	delta, err := decompressDelta(deltaData)
	if err != nil {
		return fmt.Errorf("invalid delta data: %w", err)
	}

	if len(delta) != expectedSize {
		return fmt.Errorf("delta size mismatch: expected %d, got %d", expectedSize, len(delta))
	}

	return nil
}
