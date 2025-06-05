package imagestore

import (
	"fmt"
	"math"
	"sort"
)

// TileFeatures represents features extracted from a tile for similarity matching
type TileFeatures struct {
	TileID         TileID
	ColorHistogram [64]float64 // 4x4x4 RGB histogram (simplified)
	AvgBrightness  float64
	AvgRed         float64
	AvgGreen       float64
	AvgBlue        float64
	Contrast       float64
}

// SimilarityMatcher manages tile similarity search
type SimilarityMatcher struct {
	features []TileFeatures
}

// NewSimilarityMatcher creates a new similarity matcher
func NewSimilarityMatcher() *SimilarityMatcher {
	return &SimilarityMatcher{
		features: make([]TileFeatures, 0),
	}
}

// AddTile adds a tile to the similarity index
func (sm *SimilarityMatcher) AddTile(tileID TileID, tileData []byte, tileSize int) error {
	features, err := ExtractTileFeatures(tileID, tileData, tileSize)
	if err != nil {
		return fmt.Errorf("failed to extract features for tile %s: %w", tileID, err)
	}

	sm.features = append(sm.features, *features)
	return nil
}

// FindSimilarTile finds the most similar tile to the given tile data
func (sm *SimilarityMatcher) FindSimilarTile(tileData []byte, tileSize int, threshold float64) (*TileID, float64, error) {
	if len(sm.features) == 0 {
		return nil, 0, fmt.Errorf("no tiles in similarity index")
	}

	// Extract features for the query tile
	queryFeatures, err := ExtractTileFeatures("", tileData, tileSize)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to extract query features: %w", err)
	}

	bestTileID := ""
	bestDistance := math.Inf(1)

	// Find the tile with minimum feature distance
	for _, features := range sm.features {
		distance := ComputeFeatureDistance(queryFeatures, &features)
		if distance < bestDistance {
			bestDistance = distance
			bestTileID = string(features.TileID)
		}
	}

	if bestDistance <= threshold {
		tileID := TileID(bestTileID)
		return &tileID, bestDistance, nil
	}

	return nil, bestDistance, nil
}

// FindTopSimilarTiles finds the top N most similar tiles
func (sm *SimilarityMatcher) FindTopSimilarTiles(tileData []byte, tileSize int, topN int) ([]TileID, []float64, error) {
	if len(sm.features) == 0 {
		return nil, nil, fmt.Errorf("no tiles in similarity index")
	}

	// Extract features for the query tile
	queryFeatures, err := ExtractTileFeatures("", tileData, tileSize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract query features: %w", err)
	}

	type similarity struct {
		tileID   TileID
		distance float64
	}

	similarities := make([]similarity, len(sm.features))

	// Calculate distances to all tiles
	for i, features := range sm.features {
		distance := ComputeFeatureDistance(queryFeatures, &features)
		similarities[i] = similarity{
			tileID:   features.TileID,
			distance: distance,
		}
	}

	// Sort by distance (ascending)
	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].distance < similarities[j].distance
	})

	// Return top N results
	n := min(topN, len(similarities))
	tileIDs := make([]TileID, n)
	distances := make([]float64, n)

	for i := 0; i < n; i++ {
		tileIDs[i] = similarities[i].tileID
		distances[i] = similarities[i].distance
	}

	return tileIDs, distances, nil
}

// RemoveTile removes a tile from the similarity index
func (sm *SimilarityMatcher) RemoveTile(tileID TileID) {
	for i, features := range sm.features {
		if features.TileID == tileID {
			// Remove by swapping with last element
			sm.features[i] = sm.features[len(sm.features)-1]
			sm.features = sm.features[:len(sm.features)-1]
			break
		}
	}
}

// Size returns the number of tiles in the index
func (sm *SimilarityMatcher) Size() int {
	return len(sm.features)
}

// ExtractTileFeatures extracts features from tile data for similarity matching
func ExtractTileFeatures(tileID TileID, tileData []byte, tileSize int) (*TileFeatures, error) {
	expectedSize := tileSize * tileSize * 3
	if len(tileData) != expectedSize {
		return nil, fmt.Errorf("invalid tile data size: expected %d, got %d", expectedSize, len(tileData))
	}

	features := &TileFeatures{
		TileID: tileID,
	}

	// Calculate color histogram (4x4x4 = 64 bins)
	histogram := make([]int, 64)

	var totalR, totalG, totalB float64
	var minBrightness, maxBrightness float64 = 255, 0

	numPixels := tileSize * tileSize

	for i := 0; i < numPixels; i++ {
		r := float64(tileData[i*3])
		g := float64(tileData[i*3+1])
		b := float64(tileData[i*3+2])

		// Accumulate for averages
		totalR += r
		totalG += g
		totalB += b

		// Calculate brightness
		brightness := (r + g + b) / 3.0
		if brightness < minBrightness {
			minBrightness = brightness
		}
		if brightness > maxBrightness {
			maxBrightness = brightness
		}

		// Add to histogram (quantize to 4 levels per channel)
		rBin := int(r / 64) // 0-3
		gBin := int(g / 64) // 0-3
		bBin := int(b / 64) // 0-3

		if rBin > 3 {
			rBin = 3
		}
		if gBin > 3 {
			gBin = 3
		}
		if bBin > 3 {
			bBin = 3
		}

		histIndex := rBin*16 + gBin*4 + bBin
		histogram[histIndex]++
	}

	// Normalize histogram
	for i := 0; i < 64; i++ {
		features.ColorHistogram[i] = float64(histogram[i]) / float64(numPixels)
	}

	// Calculate averages
	features.AvgRed = totalR / float64(numPixels)
	features.AvgGreen = totalG / float64(numPixels)
	features.AvgBlue = totalB / float64(numPixels)
	features.AvgBrightness = (totalR + totalG + totalB) / (3.0 * float64(numPixels))

	// Calculate contrast
	features.Contrast = maxBrightness - minBrightness

	return features, nil
}

// ComputeFeatureDistance calculates the distance between two tile feature sets
func ComputeFeatureDistance(f1, f2 *TileFeatures) float64 {
	var distance float64

	// Histogram distance (chi-squared)
	histDistance := 0.0
	for i := 0; i < 64; i++ {
		sum := f1.ColorHistogram[i] + f2.ColorHistogram[i]
		if sum > 0 {
			diff := f1.ColorHistogram[i] - f2.ColorHistogram[i]
			histDistance += (diff * diff) / sum
		}
	}
	histDistance *= 0.5

	// Color average distances
	avgRedDiff := (f1.AvgRed - f2.AvgRed) / 255.0
	avgGreenDiff := (f1.AvgGreen - f2.AvgGreen) / 255.0
	avgBlueDiff := (f1.AvgBlue - f2.AvgBlue) / 255.0
	avgBrightnessDiff := (f1.AvgBrightness - f2.AvgBrightness) / 255.0

	colorDistance := math.Sqrt(avgRedDiff*avgRedDiff + avgGreenDiff*avgGreenDiff + avgBlueDiff*avgBlueDiff)

	// Contrast difference
	contrastDiff := (f1.Contrast - f2.Contrast) / 255.0

	// Weighted combination
	distance = 0.4*histDistance + 0.4*colorDistance + 0.1*math.Abs(avgBrightnessDiff) + 0.1*math.Abs(contrastDiff)

	return distance
}

// BestMatchWithPixelCheck finds the best match and verifies with actual pixel comparison
func (sm *SimilarityMatcher) BestMatchWithPixelCheck(tileData []byte, tileSize int, featureThreshold, pixelThreshold float64, getTileData func(TileID) ([]byte, error)) (*TileID, float64, error) {
	// First, find candidates using feature similarity
	candidates, distances, err := sm.FindTopSimilarTiles(tileData, tileSize, 5) // Check top 5 candidates
	if err != nil {
		return nil, 0, err
	}

	bestTileID := ""
	bestPixelDistance := math.Inf(1)

	// Check pixel-level similarity for candidates
	for i, candidateID := range candidates {
		if distances[i] > featureThreshold {
			break // Remaining candidates are too far in feature space
		}

		// Get candidate tile data
		candidateData, err := getTileData(candidateID)
		if err != nil {
			continue // Skip this candidate
		}

		// Compute pixel-level distance
		pixelDistance, err := ComputePerceptualDistance(tileData, candidateData, tileSize)
		if err != nil {
			continue // Skip this candidate
		}

		if pixelDistance < bestPixelDistance {
			bestPixelDistance = pixelDistance
			bestTileID = string(candidateID)
		}
	}

	if bestPixelDistance <= pixelThreshold && bestTileID != "" {
		tileID := TileID(bestTileID)
		return &tileID, bestPixelDistance, nil
	}

	return nil, bestPixelDistance, nil
}
