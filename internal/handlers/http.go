package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gordyf/imageencoder/lib/imagestore"
)

// ImageHandler handles HTTP requests for the image store
type ImageHandler struct {
	store imagestore.ImageStore
}

// NewImageHandler creates a new image handler
func NewImageHandler(store imagestore.ImageStore) *ImageHandler {
	return &ImageHandler{
		store: store,
	}
}

// RegisterRoutes registers all HTTP routes
func (h *ImageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/images/", h.handleImages)
	mux.HandleFunc("/images", h.handleImagesList)
	mux.HandleFunc("/stats", h.handleStats)
	mux.HandleFunc("/health", h.handleHealth)
}

// handleImages handles individual image operations
func (h *ImageHandler) handleImages(w http.ResponseWriter, r *http.Request) {
	// Extract image ID from path
	path := strings.TrimPrefix(r.URL.Path, "/images/")
	if path == "" {
		http.Error(w, "Missing image ID", http.StatusBadRequest)
		return
	}

	imageID := path

	switch r.Method {
	case http.MethodPost:
		h.storeImage(w, r, imageID)
	case http.MethodGet:
		h.retrieveImage(w, imageID)
	case http.MethodDelete:
		h.deleteImage(w, imageID)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleImagesList handles listing all images
func (h *ImageHandler) handleImagesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	imageIDs, err := h.store.ListImages()
	if err != nil {
		log.Printf("Error listing images: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"images": imageIDs,
		"count":  len(imageIDs),
	})
}

// storeImage handles POST /images/{id}
func (h *ImageHandler) storeImage(w http.ResponseWriter, r *http.Request, imageID string) {
	// Parse multipart form
	err := r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Get file from form
	file, fileHeader, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Missing image file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file type
	contentType := fileHeader.Header.Get("Content-Type")
	if !isValidImageType(contentType) {
		http.Error(w, "Invalid image type. Supported: PNG, JPEG", http.StatusBadRequest)
		return
	}

	// Read file data
	imageData, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Error reading image data: %v", err)
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	// Validate file size
	if len(imageData) > 50<<20 { // 50MB max
		http.Error(w, "Image too large (max 50MB)", http.StatusRequestEntityTooLarge)
		return
	}

	// Store image
	err = h.store.StoreImage(imageID, imageData)
	if err != nil {
		log.Printf("Error storing image %s: %v", imageID, err)
		http.Error(w, "Failed to store image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"image_id": imageID,
		"message":  "Image stored successfully",
	})
}

// retrieveImage handles GET /images/{id}
func (h *ImageHandler) retrieveImage(w http.ResponseWriter, imageID string) {
	imageData, err := h.store.RetrieveImage(imageID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Image not found", http.StatusNotFound)
			return
		}
		log.Printf("Error retrieving image %s: %v", imageID, err)
		http.Error(w, "Failed to retrieve image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s.png\"", imageID))
	w.Write(imageData)
}

// deleteImage handles DELETE /images/{id}
func (h *ImageHandler) deleteImage(w http.ResponseWriter, imageID string) {
	err := h.store.DeleteImage(imageID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Image not found", http.StatusNotFound)
			return
		}
		log.Printf("Error deleting image %s: %v", imageID, err)
		http.Error(w, "Failed to delete image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"image_id": imageID,
		"message":  "Image deleted successfully",
	})
}

// handleStats handles GET /stats
func (h *ImageHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.store.GetStorageStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleHealth handles GET /health
func (h *ImageHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "imageencoder",
	})
}

// isValidImageType checks if the content type is a supported image format
func isValidImageType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/jpg":
		return true
	default:
		return false
	}
}

// CORSMiddleware adds CORS headers
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
