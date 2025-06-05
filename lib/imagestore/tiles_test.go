package imagestore

import (
	"image"
	"image/color"
	"testing"
)

func TestExtractTiles(t *testing.T) {
	// Create a 10x10 test image
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 25), uint8(y * 25), 128, 255})
		}
	}

	tileSize := 4
	tiles, tileRefs, err := ExtractTiles(img, tileSize)
	if err != nil {
		t.Fatalf("failed to extract tiles: %v", err)
	}

	// For a 10x10 image with 4x4 tiles, we expect 3x3 = 9 tiles (ceil(10/4) = 3)
	expectedTiles := 9
	if len(tiles) != expectedTiles {
		t.Errorf("expected %d tiles, got %d", expectedTiles, len(tiles))
	}

	if len(tileRefs) != expectedTiles {
		t.Errorf("expected %d tile refs, got %d", expectedTiles, len(tileRefs))
	}

	// Verify each tile has correct data size
	expectedDataSize := tileSize * tileSize * 3
	for i, tile := range tiles {
		if len(tile.Data) != expectedDataSize {
			t.Errorf("tile %d has incorrect data size: expected %d, got %d", i, expectedDataSize, len(tile.Data))
		}

		// Verify tile ID generation
		expectedHash := ComputeTileHash(tile.Data)
		if tile.Hash != expectedHash {
			t.Errorf("tile %d has incorrect hash", i)
		}

		expectedID := GenerateTileID(tile.Hash)
		if tile.ID != expectedID {
			t.Errorf("tile %d has incorrect ID", i)
		}
	}

	// Verify tile references have correct coordinates
	tileIndex := 0
	for tileY := 0; tileY < 3; tileY++ {
		for tileX := 0; tileX < 3; tileX++ {
			if tileRefs[tileIndex].X != tileX {
				t.Errorf("tile ref %d has incorrect X coordinate: expected %d, got %d", tileIndex, tileX, tileRefs[tileIndex].X)
			}
			if tileRefs[tileIndex].Y != tileY {
				t.Errorf("tile ref %d has incorrect Y coordinate: expected %d, got %d", tileIndex, tileY, tileRefs[tileIndex].Y)
			}
			tileIndex++
		}
	}
}

func TestExtractTileDataWithPadding(t *testing.T) {
	// Create a 3x3 image
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 85), uint8(y * 85), 0, 255})
		}
	}

	tileSize := 4
	// Extract from top-left corner (0,0) to (3,3) but with 4x4 tile size
	tileData := extractTileData(img, 0, 0, 3, 3, tileSize)

	expectedSize := tileSize * tileSize * 3
	if len(tileData) != expectedSize {
		t.Fatalf("expected tile data size %d, got %d", expectedSize, len(tileData))
	}

	// Check that the actual image data is correct
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			i := (y*tileSize + x) * 3
			expectedR := uint8(x * 85)
			expectedG := uint8(y * 85)
			expectedB := uint8(0)

			if tileData[i] != expectedR || tileData[i+1] != expectedG || tileData[i+2] != expectedB {
				t.Errorf("pixel (%d,%d) mismatch: expected RGB(%d,%d,%d), got RGB(%d,%d,%d)",
					x, y, expectedR, expectedG, expectedB, tileData[i], tileData[i+1], tileData[i+2])
			}
		}
	}

	// Check that padding area (beyond 3x3) is zero
	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			if x >= 3 || y >= 3 {
				i := (y*tileSize + x) * 3
				if tileData[i] != 0 || tileData[i+1] != 0 || tileData[i+2] != 0 {
					t.Errorf("padding pixel (%d,%d) should be zero, got RGB(%d,%d,%d)",
						x, y, tileData[i], tileData[i+1], tileData[i+2])
				}
			}
		}
	}
}

func TestReconstructImage(t *testing.T) {
	// Create original 8x8 image
	originalImg := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r := uint8((x + y) * 32 % 256)
			g := uint8((x * y) % 256)
			b := uint8((x - y + 8) * 32 % 256)
			originalImg.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}

	tileSize := 4
	tiles, tileRefs, err := ExtractTiles(originalImg, tileSize)
	if err != nil {
		t.Fatalf("failed to extract tiles: %v", err)
	}

	// Create stored image structure
	storedImage := &StoredImage{
		ID:       "test",
		Width:    8,
		Height:   8,
		TileRefs: tileRefs,
	}

	// Create tile data lookup
	tileDataMap := make(map[TileID][]byte)
	for _, tile := range tiles {
		tileDataMap[tile.ID] = tile.Data
	}

	getTileData := func(tileID TileID) ([]byte, error) {
		data, exists := tileDataMap[tileID]
		if !exists {
			t.Fatalf("tile data not found for ID: %s", tileID)
		}
		return data, nil
	}

	// Reconstruct image
	reconstructed, err := ReconstructImage(storedImage, tileSize, getTileData)
	if err != nil {
		t.Fatalf("failed to reconstruct image: %v", err)
	}

	// Verify dimensions
	bounds := reconstructed.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("reconstructed image size mismatch: expected 8x8, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Verify pixel values match original
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			originalR, originalG, originalB, _ := originalImg.At(x, y).RGBA()
			reconstructedR, reconstructedG, reconstructedB, _ := reconstructed.At(x, y).RGBA()

			if originalR != reconstructedR || originalG != reconstructedG || originalB != reconstructedB {
				t.Errorf("pixel (%d,%d) mismatch: original RGBA(%d,%d,%d), reconstructed RGBA(%d,%d,%d)",
					x, y, originalR>>8, originalG>>8, originalB>>8,
					reconstructedR>>8, reconstructedG>>8, reconstructedB>>8)
			}
		}
	}
}

func TestPlaceTileData(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	tileSize := 4

	// Create test tile data (4x4 red tile)
	tileData := make([]byte, tileSize*tileSize*3)
	for i := 0; i < len(tileData); i += 3 {
		tileData[i] = 255 // R
		tileData[i+1] = 0 // G
		tileData[i+2] = 0 // B
	}

	// Place tile at position (2, 2)
	err := placeTileData(img, tileData, 2, 2, tileSize, 8, 8)
	if err != nil {
		t.Fatalf("failed to place tile data: %v", err)
	}

	// Verify tile was placed correctly
	for y := 2; y < 6; y++ { // 2 to 5 (4x4 tile)
		for x := 2; x < 6; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
				t.Errorf("pixel (%d,%d) should be red, got RGBA(%d,%d,%d,%d)",
					x, y, r>>8, g>>8, b>>8, a>>8)
			}
		}
	}

	// Verify pixels outside tile area are still black (default)
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 0 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("pixel (0,0) should be black, got RGB(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestPlaceTileDataInvalidSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	tileSize := 4

	// Create tile data with wrong size
	invalidTileData := make([]byte, 10) // Should be 4*4*3 = 48 bytes

	err := placeTileData(img, invalidTileData, 0, 0, tileSize, 8, 8)
	if err == nil {
		t.Error("expected error for invalid tile data size, got nil")
	}
}

func TestCreateEmptyTile(t *testing.T) {
	tileSize := 4
	emptyTile := CreateEmptyTile(tileSize)

	expectedSize := tileSize * tileSize * 3
	if len(emptyTile) != expectedSize {
		t.Errorf("expected empty tile size %d, got %d", expectedSize, len(emptyTile))
	}

	// Verify all bytes are zero
	for i, b := range emptyTile {
		if b != 0 {
			t.Errorf("empty tile byte %d should be 0, got %d", i, b)
		}
	}
}

func TestValidateTileData(t *testing.T) {
	tileSize := 4

	// Valid tile data
	validData := make([]byte, tileSize*tileSize*3)
	err := ValidateTileData(validData, tileSize)
	if err != nil {
		t.Errorf("valid tile data should pass validation, got error: %v", err)
	}

	// Invalid tile data (wrong size)
	invalidData := make([]byte, 10)
	err = ValidateTileData(invalidData, tileSize)
	if err == nil {
		t.Error("invalid tile data should fail validation")
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{5, 3, 3},
		{3, 5, 3},
		{0, 0, 0},
		{-1, 2, -1},
		{10, 10, 10},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}
