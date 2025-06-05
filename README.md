# Image Encoder - Tile Dictionary + Delta Encoding Image Storage System

A Go library and HTTP service for efficient storage of similar images using tile-based deduplication with delta encoding. Designed for website screenshots and other collections of similar images.

## Features

- **Tile-based deduplication**: Images are divided into 256x256 pixel tiles
- **Delta encoding**: Similar tiles are stored as deltas from existing tiles
- **Content-addressed storage**: Unique tiles stored once using SHA-256 hashing
- **HTTP API**: RESTful API for storing, retrieving, and managing images
- **Sub-linear storage growth**: Efficient storage for collections of similar images
- **BoltDB backend**: Embedded database for persistence

## Quick Start

### Building

```bash
go build ./cmd/server
```

### Running the Server

```bash
# Start with default configuration
./server

# Start with custom configuration
./server -config myconfig.json

# Start with command line overrides
./server -port 9090 -host 0.0.0.0 -db /tmp/images.db

# Show help
./server --help
```

### Configuration

Create a `config.json` file:

```json
{
  "server": {
    "port": 8080,
    "host": "localhost",
    "read_timeout_seconds": 30,
    "write_timeout_seconds": 30
  },
  "image_store": {
    "tile_size": 256,
    "similarity_threshold": 0.1,
    "database_path": "./imagestore.db"
  },
  "log_level": "info"
}
```

## API Usage

### Store an Image

```bash
curl -X POST \
  -F "image=@screenshot.png" \
  http://localhost:8080/images/my-screenshot-id
```

### Retrieve an Image

```bash
curl http://localhost:8080/images/my-screenshot-id > retrieved.png
```

### List All Images

```bash
curl http://localhost:8080/images
```

### Get Debug Visualization

```bash
curl http://localhost:8080/debug/my-screenshot-id > debug.png
```

The debug image shows color-coded tiles:
- **Green**: Unique tiles (newly stored)
- **Blue**: Duplicate tiles (exact hash match)
- **Yellow**: Delta-encoded tiles (stored as difference from similar tile)
- **Red**: Error/unknown storage type

### Get Storage Statistics

```bash
curl http://localhost:8080/stats
```

### Delete an Image

```bash
curl -X DELETE http://localhost:8080/images/my-screenshot-id
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Environment Variables

You can configure the server using environment variables:

- `SERVER_PORT` - Server port (default: 8080)
- `SERVER_HOST` - Server host (default: localhost)
- `DATABASE_PATH` - Database file path (default: ./imagestore.db)
- `TILE_SIZE` - Tile size in pixels (default: 256)
- `SIMILARITY_THRESHOLD` - Similarity threshold for delta encoding (default: 0.1)
- `LOG_LEVEL` - Log level: debug, info, warn, error (default: info)

## How It Works

### Tile Dictionary Algorithm

1. **Image Tiling**: Each image is divided into fixed-size tiles (256x256 pixels)
2. **Hash-based Deduplication**: Each tile is hashed (SHA-256) and stored only once
3. **Similarity Matching**: For new tiles, the system finds the most similar existing tile
4. **Delta Encoding**: If similarity is above threshold, store only the difference (delta)
5. **Reconstruction**: Images are rebuilt by assembling tiles and applying deltas

### Storage Layout

The system uses BoltDB with the following buckets:
- `tiles` - Unique tile data indexed by tile ID
- `deltas` - Delta data for similar tiles
- `images` - Image metadata and tile references
- `features` - Tile features for similarity matching

### Performance Characteristics

- **Storage Efficiency**: Sub-linear growth for similar images
- **Retrieval Speed**: <100ms for typical screenshot reconstruction
- **Insertion Time**: <500ms for processing and storage
- **Memory Usage**: Efficient - doesn't load entire tile dictionary into RAM

## Project Structure

```
cmd/
  server/main.go          - HTTP server entry point
lib/
  imagestore/
    store.go              - Core types and interfaces
    tiles.go              - Tile extraction/reconstruction
    delta.go              - Delta computation and encoding
    similarity.go         - Tile similarity matching
    storage.go            - BoltDB persistence layer
  config/config.go        - Configuration management
internal/
  handlers/http.go        - HTTP request handlers
  utils/image.go          - Image processing utilities
```

## Library Usage

You can also use the image store as a Go library:

```go
package main

import (
    "github.com/gordyf/imageencoder/lib/imagestore"
)

func main() {
    // Create store
    config := imagestore.DefaultConfig()
    store, err := imagestore.NewBoltImageStore(config)
    if err != nil {
        panic(err)
    }
    defer store.Close()

    // Store an image
    imageData := []byte{...} // your image data
    err = store.StoreImage("my-image", imageData)
    if err != nil {
        panic(err)
    }

    // Retrieve an image
    retrievedData, err := store.RetrieveImage("my-image")
    if err != nil {
        panic(err)
    }

    // Get statistics
    stats := store.GetStorageStats()
    fmt.Printf("Compression ratio: %.2f\n", stats.CompressionRatio)
}
```

## License

MIT License