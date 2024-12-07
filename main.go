package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"net/http"
	"os"

	svg "github.com/ajstarks/svgo"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
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

func loadFont() (*truetype.Font, error) {
    fontBytes, err := os.ReadFile("./fonts/roboto.ttf")
    if err != nil {
        return nil, err
    }
    f, err := freetype.ParseFont(fontBytes)
    if err != nil {
        return nil, err
    }
    return f, nil
}

// Function to calculate the drawing range
func calculateBounds(fc *geojson.FeatureCollection, scaleMap map[int]int) (minLon, minLat, maxLon, maxLat float64) {
    minLon = 180.0
    minLat = 90.0
    maxLon = -180.0
    maxLat = -90.0

    for _, feature := range fc.Features {
        // Skip if the scale is 0 (transparent prefectures are not calculated)
        id := int(feature.Properties["id"].(float64))
        if scaleMap[id] == 0 {
            continue
        }

        // Calculate the range from the coordinates of the polygon
        switch feature.Geometry.Type {
        case "Polygon":
            for _, ring := range feature.Geometry.Polygon {
                for _, coord := range ring {
                    lon, lat := coord[0], coord[1]
                    minLon = min(minLon, lon)
                    minLat = min(minLat, lat)
                    maxLon = max(maxLon, lon)
                    maxLat = max(maxLat, lat)
                }
            }
        case "MultiPolygon":
            for _, polygon := range feature.Geometry.MultiPolygon {
                for _, ring := range polygon {
                    for _, coord := range ring {
                        lon, lat := coord[0], coord[1]
                        minLon = min(minLon, lon)
                        minLat = min(minLat, lat)
                        maxLon = max(maxLon, lon)
                        maxLat = max(maxLat, lat)
                    }
                }
            }
        }
    }
    return
}

// Function to convert SVG data to PNG
func svgToPNG(svgData []byte, width, height int, footerText string) ([]byte, error) {
    // Loading SVG data
    icon, err := oksvg.ReadIconStream(bytes.NewReader(svgData))
    if err != nil {
        return nil, fmt.Errorf("failed to read icon stream: %w", err)
    }

    // Drawing Area Settings
    icon.SetTarget(0, 0, float64(width), float64(height))

    // Creating RGBA images for drawing
    rgba := image.NewRGBA(image.Rect(0, 0, width, height))
    scanner := rasterx.NewScannerGV(width, height, rgba, rgba.Bounds())
    raster := rasterx.NewDasher(width, height, scanner)

    // SVG rendering
    icon.Draw(raster, 1.0)

	if footerText == "" {
		footerText = "Code available under the MIT License (GitHub: evacuate)."
	}

	// Load the font
	f, err := loadFont()
	if err != nil {
		return nil, fmt.Errorf("failed to load font: %w", err)
	}
	
	// Create a new context
	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(f)
	c.SetFontSize(14)
	c.SetClip(rgba.Bounds())
	c.SetDst(rgba)
	c.SetSrc(image.NewUniform(color.RGBA{0xfa, 0xfa, 0xfa, 0xff}))

	// Draw the text
	pt := freetype.Pt(10, height-14)
	_, err = c.DrawString(footerText, pt)
	if err != nil {
		return nil, fmt.Errorf("failed to draw text: %w", err)
	}

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

	// Calculate the valid area
	minLon, minLat, maxLon, maxLat := calculateBounds(fc, scaleMap)

	funcToScreen := func(lon, lat float64) (x, y float64) {
		// Calculate the effective drawing area
		margin := 0.1
		effectiveWidth := CANVAS_WIDTH * (1.0 - 2*margin)
		effectiveHeight := CANVAS_HEIGHT * (1.0 - 2*margin)
	
		// Calculate center coordinates only once
		centerLat := (maxLat + minLat) / 2
		centerLon := (maxLon + minLon) / 2
		centerX := CANVAS_WIDTH / 2
		centerY := CANVAS_HEIGHT / 2
	
		// Calculate the correction factor for longitude distance by latitude
		lonCorrection := math.Cos(centerLat * math.Pi / 180.0)
	
		lonSpan := (maxLon - minLon) * lonCorrection  // Correct longitude range
		latSpan := maxLat - minLat
		
		scaleX := effectiveWidth / lonSpan
		scaleY := effectiveHeight / latSpan
		scale := min(scaleX, scaleY)
	
		x = ((lon - centerLon) * lonCorrection) * scale + centerX
		y = (centerLat - lat) * scale + centerY
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

	footerText := r.URL.Query().Get("footer")

	canvas.End()

	format := r.URL.Query().Get("format")
	if format == "svg" {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(buf.Bytes())
		return
	}

	// Convert SVG to PNG
	pngData, err := svgToPNG(buf.Bytes(), int(CANVAS_WIDTH), int(CANVAS_HEIGHT), footerText)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to convert svg to png: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(pngData)
}

func min(a, b float64) float64 {
    if a < b {
        return a
    }
    return b
}

func max(a, b float64) float64 {
    if a > b {
        return a
    }
    return b
}

func main() {
	http.HandleFunc("/map", mapHandler)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
