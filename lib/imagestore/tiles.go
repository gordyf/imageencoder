package imagestore

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// ExtractTiles divides an image into fixed-size tiles
func ExtractTiles(img image.Image, tileSize int) ([]Tile, []TileRef, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	tilesX := int(math.Ceil(float64(width) / float64(tileSize)))
	tilesY := int(math.Ceil(float64(height) / float64(tileSize)))

	var tiles []Tile
	var tileRefs []TileRef

	for tileY := 0; tileY < tilesY; tileY++ {
		for tileX := 0; tileX < tilesX; tileX++ {
			// Calculate tile boundaries
			x0 := tileX * tileSize
			y0 := tileY * tileSize
			x1 := min(x0+tileSize, width)
			y1 := min(y0+tileSize, height)

			// Extract tile data
			tileData := extractTileData(img, x0, y0, x1, y1, tileSize)

			// Compute hash and ID
			hash := ComputeTileHash(tileData)
			tileID := GenerateTileID(hash)

			// Create tile
			tile := Tile{
				ID:   tileID,
				Hash: hash,
				Data: tileData,
			}

			// Create tile reference
			tileRef := TileRef{
				X:      tileX,
				Y:      tileY,
				TileID: tileID,
			}

			tiles = append(tiles, tile)
			tileRefs = append(tileRefs, tileRef)
		}
	}

	return tiles, tileRefs, nil
}

// extractTileData extracts RGB data from a tile region, padding if necessary
func extractTileData(img image.Image, x0, y0, x1, y1, tileSize int) []byte {
	data := make([]byte, tileSize*tileSize*3)

	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			srcX := x0 + x
			srcY := y0 + y

			var r, g, b uint8

			// If within image bounds, get actual pixel
			if srcX < x1 && srcY < y1 {
				pixel := img.At(srcX, srcY)
				rVal, gVal, bVal, _ := pixel.RGBA()
				r = uint8(rVal >> 8)
				g = uint8(gVal >> 8)
				b = uint8(bVal >> 8)
			}
			// Otherwise, pixel remains (0, 0, 0) for padding

			i := (y*tileSize + x) * 3
			data[i] = r
			data[i+1] = g
			data[i+2] = b
		}
	}

	return data
}

// ReconstructImage rebuilds an image from tiles
func ReconstructImage(storedImage *StoredImage, tileSize int, getTileData func(TileID) ([]byte, error)) (image.Image, error) {
	// Create output image
	img := image.NewRGBA(image.Rect(0, 0, storedImage.Width, storedImage.Height))

	// Place each tile
	for _, tileRef := range storedImage.TileRefs {
		// Get tile data
		tileData, err := getTileData(tileRef.TileID)
		if err != nil {
			return nil, fmt.Errorf("failed to get tile data for %s: %w", tileRef.TileID, err)
		}

		// Calculate tile position in pixels
		tileX := tileRef.X * tileSize
		tileY := tileRef.Y * tileSize

		// Place tile data into image
		err = placeTileData(img, tileData, tileX, tileY, tileSize, storedImage.Width, storedImage.Height)
		if err != nil {
			return nil, fmt.Errorf("failed to place tile at (%d, %d): %w", tileRef.X, tileRef.Y, err)
		}
	}

	return img, nil
}

// placeTileData places tile data into the image at the specified position
func placeTileData(img *image.RGBA, tileData []byte, offsetX, offsetY, tileSize, imgWidth, imgHeight int) error {
	if len(tileData) != tileSize*tileSize*3 {
		return fmt.Errorf("invalid tile data size: expected %d, got %d", tileSize*tileSize*3, len(tileData))
	}

	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			imgX := offsetX + x
			imgY := offsetY + y

			// Only place pixels within image bounds
			if imgX < imgWidth && imgY < imgHeight {
				i := (y*tileSize + x) * 3
				r := tileData[i]
				g := tileData[i+1]
				b := tileData[i+2]

				img.Set(imgX, imgY, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}

	return nil
}

// CreateEmptyTile creates a tile filled with zeros (black)
func CreateEmptyTile(tileSize int) []byte {
	return make([]byte, tileSize*tileSize*3)
}

// ValidateTileData checks if tile data has the correct size
func ValidateTileData(data []byte, tileSize int) error {
	expected := tileSize * tileSize * 3
	if len(data) != expected {
		return fmt.Errorf("invalid tile data size: expected %d bytes, got %d", expected, len(data))
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
