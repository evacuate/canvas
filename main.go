package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"

	svg "github.com/ajstarks/svgo"
	geojson "github.com/paulmach/go.geojson"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

type IntensityQuery struct {
    ID       int `json:"id"`
    Scale    int `json:"scale"`
}

// Function to convert intensity scale to color
func intensityToColor(scale int) string {
	switch scale {
	case 0:
		return "#27272a"
	case 1:
		return "#bae6fd"
	case 2:
		return "#4ade80"
	case 3:
		return "#facc15"
	case 4:
		return "#f97316"
	case 5:
		return "#dc2626"
	case 6:
		return "#86198f"
	case 7:
		return "#500724"
	default:
		if scale > 6 {
			return "#4a044e"
		}
		if scale > 5 {
			return "#b91c1c"
		}
		return "#27272a"
	}
}

// Function to convert SVG data to PNG
func svgToPNG(svgData []byte, width, height int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svgData))
	if err != nil {
		return nil, fmt.Errorf("failed to parse svg: %w", err)
	}

	// et the target area
	icon.SetTarget(0, 0, float64(width), float64(height))

	// Create an image for drawing
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, rgba, rgba.Bounds())
	raster := rasterx.NewDasher(width, height, scanner)

	// Draw the SVG
	icon.Draw(raster, 1.0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, fmt.Errorf("failed to encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	scaleData := r.URL.Query().Get("scale")
    if scaleData == "" {
        http.Error(w, "scale parameter is required", http.StatusBadRequest)
        return
    }

	var intensities []IntensityQuery
    if err := json.Unmarshal([]byte(scaleData), &intensities); err != nil {
        http.Error(w, fmt.Sprintf("Invalid scale data format: %v", err), http.StatusBadRequest)
        return
    }

	scaleMap := make(map[int]int)
    for _, intensity := range intensities {
        // Check the intensity value
        if intensity.Scale < 0 || intensity.Scale > 7 {
            http.Error(w, fmt.Sprintf("Invalid scale value for ID %d: %d", 
                intensity.ID, intensity.Scale), http.StatusBadRequest)
            return
        }
        scaleMap[intensity.ID] = intensity.Scale
    }

	const (
		CANVAS_WIDTH  = 1280.0
		CANVAS_HEIGHT = 720.0
	)

	// Map display area
	const (
		MAP_WIDTH  = 500.0
		MAP_HEIGHT = 550.0
	)

	// Range of Japan
	const (
		LON_MIN = 122.0
		LON_MAX = 146.0
		LAT_MIN = 24.0
		LAT_MAX = 46.0
	)

	data, err := os.ReadFile("japan.geojson")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read geojson: %v", err), http.StatusInternalServerError)
		return
	}

	fc, err := geojson.UnmarshalFeatureCollection(data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal geojson: %v", err), http.StatusInternalServerError)
		return
	}

	funcToScreen := func(lon, lat float64) (x, y float64) {
		marginX := (CANVAS_WIDTH - MAP_WIDTH) / 2
		marginY := (CANVAS_HEIGHT - MAP_HEIGHT) / 2

		lonScale := MAP_WIDTH / (LON_MAX - LON_MIN)
		latScale := MAP_HEIGHT / (LAT_MAX - LAT_MIN)

		x = (lon - LON_MIN) * lonScale + marginX
		y = (LAT_MAX - lat) * latScale + marginY
		return
	}

	buf := new(bytes.Buffer)
	canvas := svg.New(buf)
	canvas.Start(int(CANVAS_WIDTH), int(CANVAS_HEIGHT))
	canvas.Rect(0, 0, int(CANVAS_WIDTH), int(CANVAS_HEIGHT), "fill:#18181b")

    for _, feature := range fc.Features {
        id, ok := feature.Properties["id"].(float64)
        if !ok {
            http.Error(w, "Invalid ID format in GeoJSON", http.StatusInternalServerError)
            return
        }

        scaleValue := 0
        if val, ok := scaleMap[int(id)]; ok {
            scaleValue = val
        }
        fillColor := intensityToColor(scaleValue)

		var paths []string
		if feature.Geometry.Type == "Polygon" {
			for _, ring := range feature.Geometry.Polygon {
				var pathStr = "M"
				for i, coord := range ring {
					x, y := funcToScreen(coord[0], coord[1])
					if i == 0 {
						pathStr += fmt.Sprintf("%.1f %.1f", x, y)
					} else {
						pathStr += fmt.Sprintf(" L%.1f %.1f", x, y)
					}
				}
				pathStr += " Z"
				paths = append(paths, pathStr)
			}
		} else if feature.Geometry.Type == "MultiPolygon" {
			for _, polygon := range feature.Geometry.MultiPolygon {
				for _, ring := range polygon {
					var pathStr = "M"
					for i, coord := range ring {
						x, y := funcToScreen(coord[0], coord[1])
						if i == 0 {
							pathStr += fmt.Sprintf("%.1f %.1f", x, y)
						} else {
							pathStr += fmt.Sprintf(" L%.1f %.1f", x, y)
						}
					}
					pathStr += " Z"
					paths = append(paths, pathStr)
				}
			}
		}

		finalPath := ""
		for _, p := range paths {
			finalPath += p + " "
		}

		style := fmt.Sprintf("fill:%s;stroke:#a1a1aa;stroke-width:0.2;fill-opacity:0.8", fillColor)
		canvas.Path(finalPath, style)
	}

	canvas.End()

	// Convert SVG to PNG
	pngData, err := svgToPNG(buf.Bytes(), int(CANVAS_WIDTH), int(CANVAS_HEIGHT))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to convert svg to png: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(pngData)
}

func main() {
	http.HandleFunc("/map", mapHandler)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
